package store

// Recording is a single captured/imported audio item plus its metadata.
//
// ID and CreatedAt are int64 in Go but marshalled as `string` over JSON/Wails:
// CreatedAt is a Unix-nanosecond timestamp (~1.7e18, far past JS's 2^53 safe-int
// ceiling) and IDs can grow large. Sending them as numbers would let JS silently
// round them and mutate the wrong row — Scribe's known int64-over-JSON gotcha.
type Recording struct {
	ID          int64   `json:"id,string"`
	Title       string  `json:"title"`
	AudioPath   string  `json:"audioPath"`
	CreatedAt   int64   `json:"createdAt,string"` // Unix nanoseconds
	DurationSec float64 `json:"durationSec"`
	Engine      string  `json:"engine"` // which Transcriber produced the segments
}

// Segment is one transcript line belonging to a Recording.
//
// Start/End are seconds (float64); RecordingID is marshalled as string for the
// same precision reason as Recording.ID.
type Segment struct {
	ID          int64   `json:"id,string"`
	RecordingID int64   `json:"recordingId,string"`
	Start       float64 `json:"start"`
	End         float64 `json:"end"`
	Text        string  `json:"text"`
}

// SearchHit is a full-text match: the matching segment plus enough of its parent
// recording to render a result row without a second round-trip.
type SearchHit struct {
	RecordingID    int64   `json:"recordingId,string"`
	RecordingTitle string  `json:"recordingTitle"`
	SegmentID      int64   `json:"segmentId,string"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
	Text           string  `json:"text"`
}
