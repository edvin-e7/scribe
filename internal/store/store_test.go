package store

import (
	"testing"
)

// newTestStore opens a fresh in-memory store (modernc driver, no CGO). Each test
// gets its own DB so there is no cross-test bleed.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestFTSRoundTrip is the load-bearing test: persist transcript segments, then
// prove search FINDS a present term AND MISSES an absent one. The miss assertion
// is the falsifying half — a broken index that returned everything would pass a
// hit-only test, so we assert the absent term yields zero results.
func TestFTSRoundTrip(t *testing.T) {
	s := newTestStore(t)

	rec, err := s.CreateRecording("Standup meeting", "/tmp/standup.wav", "stub", 12.5, 0)
	if err != nil {
		t.Fatalf("CreateRecording: %v", err)
	}

	segs := []Segment{
		{Start: 0.0, End: 3.0, Text: "We shipped the transcription pipeline today"},
		{Start: 3.0, End: 6.5, Text: "Privacy stays on device, nothing uploaded"},
		{Start: 6.5, End: 9.0, Text: "Next block wires whisper into the engine"},
	}
	if err := s.ReplaceSegments(rec.ID, segs); err != nil {
		t.Fatalf("ReplaceSegments: %v", err)
	}

	// PRESENT term must hit, and the hit must carry the right parent recording.
	hits, err := s.Search("whisper")
	if err != nil {
		t.Fatalf("Search(present): %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Search(whisper): want 1 hit, got %d (%+v)", len(hits), hits)
	}
	if hits[0].RecordingID != rec.ID {
		t.Errorf("hit RecordingID = %d, want %d", hits[0].RecordingID, rec.ID)
	}
	if hits[0].RecordingTitle != "Standup meeting" {
		t.Errorf("hit title = %q, want %q", hits[0].RecordingTitle, "Standup meeting")
	}

	// FALSIFYING half: a term that appears in NO segment must return zero hits.
	// If this ever returns results, the FTS index is matching the wrong thing.
	absent, err := s.Search("kangaroo")
	if err != nil {
		t.Fatalf("Search(absent): %v", err)
	}
	if len(absent) != 0 {
		t.Fatalf("Search(kangaroo): want 0 hits, got %d (%+v)", len(absent), absent)
	}

	// A second present term that spans a different segment also hits.
	priv, err := s.Search("privacy")
	if err != nil {
		t.Fatalf("Search(privacy): %v", err)
	}
	if len(priv) != 1 {
		t.Errorf("Search(privacy): want 1 hit, got %d", len(priv))
	}
}

// TestReplaceSegmentsReindexes proves re-transcribing a recording does not leave
// stale FTS rows: the old term must STOP matching after replacement.
func TestReplaceSegmentsReindexes(t *testing.T) {
	s := newTestStore(t)
	rec, err := s.CreateRecording("Re-run", "/tmp/x.wav", "stub", 5, 0)
	if err != nil {
		t.Fatalf("CreateRecording: %v", err)
	}

	if err := s.ReplaceSegments(rec.ID, []Segment{{Start: 0, End: 2, Text: "obsolete elephant text"}}); err != nil {
		t.Fatalf("first ReplaceSegments: %v", err)
	}
	if hits, _ := s.Search("elephant"); len(hits) != 1 {
		t.Fatalf("pre-replace: want 1 hit for elephant, got %d", len(hits))
	}

	// Replace with new content; the old term must vanish from the index.
	if err := s.ReplaceSegments(rec.ID, []Segment{{Start: 0, End: 2, Text: "fresh giraffe content"}}); err != nil {
		t.Fatalf("second ReplaceSegments: %v", err)
	}
	if hits, _ := s.Search("elephant"); len(hits) != 0 {
		t.Errorf("post-replace: want 0 hits for stale term elephant, got %d", len(hits))
	}
	if hits, _ := s.Search("giraffe"); len(hits) != 1 {
		t.Errorf("post-replace: want 1 hit for giraffe, got %d", len(hits))
	}
}

// TestListAndSegmentsOrdering proves explicit ordering: recordings newest-first,
// segments by start time.
func TestListAndSegmentsOrdering(t *testing.T) {
	s := newTestStore(t)

	older, _ := s.CreateRecording("Older", "/tmp/a.wav", "stub", 1, 1000)
	newer, _ := s.CreateRecording("Newer", "/tmp/b.wav", "stub", 1, 2000)

	recs, err := s.ListRecordings()
	if err != nil {
		t.Fatalf("ListRecordings: %v", err)
	}
	if len(recs) != 2 || recs[0].ID != newer.ID || recs[1].ID != older.ID {
		t.Fatalf("ListRecordings ordering wrong: got %+v", recs)
	}

	if err := s.ReplaceSegments(newer.ID, []Segment{
		{Start: 5.0, End: 6.0, Text: "second"},
		{Start: 1.0, End: 2.0, Text: "first"},
	}); err != nil {
		t.Fatalf("ReplaceSegments: %v", err)
	}
	segs, err := s.SegmentsForRecording(newer.ID)
	if err != nil {
		t.Fatalf("SegmentsForRecording: %v", err)
	}
	if len(segs) != 2 || segs[0].Text != "first" || segs[1].Text != "second" {
		t.Fatalf("segment ordering wrong: got %+v", segs)
	}
}

// TestDeleteRecordingCascades proves delete removes segments and FTS entries.
func TestDeleteRecordingCascades(t *testing.T) {
	s := newTestStore(t)
	rec, _ := s.CreateRecording("Doomed", "/tmp/d.wav", "stub", 1, 0)
	_ = s.ReplaceSegments(rec.ID, []Segment{{Start: 0, End: 1, Text: "platypus marker"}})

	if hits, _ := s.Search("platypus"); len(hits) != 1 {
		t.Fatalf("pre-delete: want 1 hit, got %d", len(hits))
	}
	if err := s.DeleteRecording(rec.ID); err != nil {
		t.Fatalf("DeleteRecording: %v", err)
	}
	if recs, _ := s.ListRecordings(); len(recs) != 0 {
		t.Errorf("post-delete: want 0 recordings, got %d", len(recs))
	}
	if hits, _ := s.Search("platypus"); len(hits) != 0 {
		t.Errorf("post-delete: FTS still matches, want 0, got %d", len(hits))
	}
}
