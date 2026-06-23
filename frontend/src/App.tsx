import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, Recording, Segment, SearchHit } from "./api";

// Scribe — "Transcript Console" archetype: a timeline-spine transcript tool, not a
// master-detail clone. Honest to the data we actually have (recordings, timestamped
// segments, search, duration). Speaker diarization / talk-time isn't in the model
// yet, so it's gated as a roadmap panel — never faked. The timeline is derived from
// real transcript density, and playback uses a real <audio> element.

function fmt(sec: number): string {
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${m}:${s.toString().padStart(2, "0")}`;
}

export default function App() {
  const [recordings, setRecordings] = useState<Recording[]>([]);
  const [selected, setSelected] = useState<Recording | null>(null);
  const [segments, setSegments] = useState<Segment[]>([]);
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [engine, setEngine] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [importing, setImporting] = useState(false);
  const [importPath, setImportPath] = useState("");
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const audioRef = useRef<HTMLAudioElement>(null);

  const refresh = useCallback(async () => {
    try { setRecordings(await api.listRecordings()); }
    catch (e) { setError(String(e)); }
  }, []);

  useEffect(() => {
    void refresh();
    api.engineName().then(setEngine).catch(() => setEngine("unknown"));
  }, [refresh]);

  const openRecording = useCallback(async (rec: Recording) => {
    setSelected(rec);
    setHits(null);
    setTime(0);
    setPlaying(false);
    try { setSegments(await api.getSegments(rec.id)); }
    catch (e) { setError(String(e)); }
  }, []);

  async function doImport() {
    const path = importPath.trim();
    if (!path) return;
    setError(null);
    try {
      const rec = await api.importAndTranscribe(path, "");
      setImportPath("");
      setImporting(false);
      await refresh();
      await openRecording(rec);
    } catch (e) { setError(String(e)); }
  }

  const doSearch = useCallback(async (q: string) => {
    if (!q.trim()) { setHits(null); return; }
    try { setHits(await api.search(q)); }
    catch (e) { setError(String(e)); }
  }, []);

  useEffect(() => {
    const t = setTimeout(() => void doSearch(query), query ? 220 : 0);
    return () => clearTimeout(t);
  }, [query, doSearch]);

  async function doDelete(rec: Recording) {
    try {
      await api.deleteRecording(rec.id);
      if (selected?.id === rec.id) { setSelected(null); setSegments([]); }
      await refresh();
    } catch (e) { setError(String(e)); }
  }

  // Transcript-density timeline: bar heights = chars spoken per time-bucket. This
  // is derived from the real transcript, not audio amplitude — an honest signal of
  // where the talking is dense, used as the navigable spine.
  const BARS = 120;
  const density = useMemo(() => {
    const dur = selected?.durationSec || 0;
    const bins = new Array(BARS).fill(0);
    if (dur > 0) {
      for (const s of segments) {
        const mid = (s.start + s.end) / 2;
        const i = Math.min(BARS - 1, Math.max(0, Math.floor((mid / dur) * BARS)));
        bins[i] += s.text.length;
      }
    }
    const max = Math.max(1, ...bins);
    return bins.map((v) => v / max);
  }, [segments, selected]);

  // Outline: evenly-spaced jump points labelled with the segment text at that time
  // — honest navigation derived from the transcript, not invented chapters.
  const outline = useMemo(() => {
    if (segments.length === 0) return [];
    const n = Math.min(6, segments.length);
    const step = Math.max(1, Math.floor(segments.length / n));
    const out: Segment[] = [];
    for (let i = 0; i < segments.length && out.length < n; i += step) out.push(segments[i]);
    return out;
  }, [segments]);

  const activeId = useMemo(() => {
    const seg = segments.find((s) => time >= s.start && time < s.end);
    return seg?.id ?? null;
  }, [segments, time]);

  const dur = selected?.durationSec || 0;
  const playheadPct = dur > 0 ? Math.min(100, (time / dur) * 100) : 0;

  function togglePlay() {
    const a = audioRef.current;
    if (!a) { setError("Audio playback needs the running app (local file access)."); return; }
    if (playing) { a.pause(); setPlaying(false); }
    else { a.play().then(() => setPlaying(true)).catch(() => setError("Could not play this audio file.")); }
  }

  function seekTo(sec: number) {
    setTime(sec);
    if (audioRef.current) audioRef.current.currentTime = sec;
  }

  function scrub(e: React.MouseEvent<HTMLDivElement>) {
    const r = e.currentTarget.getBoundingClientRect();
    seekTo(((e.clientX - r.left) / r.width) * dur);
  }

  return (
    <div className="sc">
      <header className="sc-bar">
        <span className="sc-logo" aria-hidden>s</span>
        <div className="sc-head">
          <div className="sc-title">{selected ? selected.title : "Scribe"}</div>
          <div className="sc-sub">
            {selected
              ? `${fmt(selected.durationSec)} · ${segments.length} segments`
              : "On-device voice & meeting transcription"}
          </div>
        </div>
        <div className="sc-spacer" />
        <span className="sc-badge priv"><i />never uploaded</span>
        <span className="sc-badge">{engine || "…"} · on-device</span>
        <label className="sc-search">
          {iSearch}
          <input
            type="search"
            placeholder="Search transcripts…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Search transcripts"
          />
        </label>
        <button className="sc-btn accent" onClick={() => setImporting((v) => !v)}>+ Import</button>
      </header>

      {importing && (
        <div className="sc-import">
          {iFile}
          <input
            autoFocus
            placeholder="Path to an audio file…"
            value={importPath}
            onChange={(e) => setImportPath(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") void doImport(); if (e.key === "Escape") setImporting(false); }}
            aria-label="Audio file path"
          />
          <button className="sc-btn accent" onClick={() => void doImport()}>Transcribe</button>
        </div>
      )}

      {selected && hits === null && (
        <>
          <div className="sc-wave" onClick={scrub} role="presentation" title="Transcript density — click to seek">
            {density.map((h, i) => (
              <i key={i} className={i <= (playheadPct / 100) * BARS ? "on" : ""} style={{ height: `${12 + h * 78}%` }} />
            ))}
            <span className="sc-playhead" style={{ left: `${playheadPct}%` }} />
          </div>
          <div className="sc-ruler"><span>0:00</span><span>{fmt(dur / 2)}</span><span>{fmt(dur)}</span></div>
        </>
      )}

      {error && <div className="sc-error" role="alert">{error}</div>}

      <main className="sc-body">
        <aside className="sc-rail">
          <h3>Recordings</h3>
          {recordings.length === 0 ? (
            <p className="sc-muted">No recordings yet — import an audio file.</p>
          ) : (
            <ul className="sc-recs">
              {recordings.map((r) => (
                <li key={r.id} className={selected?.id === r.id ? "on" : ""}>
                  <button onClick={() => void openRecording(r)}>
                    <span className="t">{r.title || "Untitled"}</span>
                    <span className="m">{fmt(r.durationSec)}</span>
                  </button>
                  <button className="x" aria-label={`Delete ${r.title}`} onClick={() => void doDelete(r)}>✕</button>
                </li>
              ))}
            </ul>
          )}

          {selected && outline.length > 0 && hits === null && (
            <>
              <h3 className="mt">Outline</h3>
              <ul className="sc-outline">
                {outline.map((s) => (
                  <li key={s.id}>
                    <button onClick={() => seekTo(s.start)}>
                      <span className="o-t">{s.text.slice(0, 34) || "—"}</span>
                      <span className="o-ts">{fmt(s.start)}</span>
                    </button>
                  </li>
                ))}
              </ul>
              <p className="sc-note">Speaker labels &amp; talk-time arrive with diarization (roadmap) — not shown until real.</p>
            </>
          )}
        </aside>

        <section className="sc-main">
          {hits !== null ? (
            <>
              <h2 className="sc-h">Search · {hits.length} result{hits.length === 1 ? "" : "s"}</h2>
              {hits.length === 0 ? <p className="sc-muted">No matches.</p> : (
                <ul className="sc-hits">
                  {hits.map((h) => (
                    <li key={h.segmentId}>
                      <button onClick={() => { const r = recordings.find((x) => x.id === h.recordingId); if (r) void openRecording(r).then(() => seekTo(h.start)); }}>
                        <span className="ts">{fmt(h.start)}</span>
                        <span className="body"><span className="src">{h.recordingTitle}</span><span className="tx">{h.text}</span></span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </>
          ) : selected ? (
            <ul className="sc-trans">
              {segments.map((s) => (
                <li key={s.id} className={s.id === activeId ? "act" : ""}>
                  <button className="ts" onClick={() => seekTo(s.start)}>{fmt(s.start)}</button>
                  <p>{s.text}</p>
                </li>
              ))}
              {segments.length === 0 && <p className="sc-muted">No transcript segments.</p>}
            </ul>
          ) : (
            <div className="sc-empty">
              <p className="big">Select a recording.</p>
              <p className="sc-muted">Transcripts stay on this machine — no audio or text is ever uploaded.</p>
            </div>
          )}
        </section>
      </main>

      {selected && (
        <footer className="sc-transport">
          <button className="sc-play" onClick={togglePlay} aria-label={playing ? "Pause" : "Play"}>{playing ? "❚❚" : "▶"}</button>
          <span className="sc-time">{fmt(time)}</span>
          <div className="sc-scrub" onClick={scrub} role="presentation"><i style={{ width: `${playheadPct}%` }} /><b style={{ left: `${playheadPct}%` }} /></div>
          <span className="sc-time">{fmt(dur)}</span>
          <span className="sc-ctrl">1.0×</span>
          <span className="sc-ctrl">Export ▾</span>
          {/* Real playback when running in the app; the standalone preview has no file. */}
          <audio
            ref={audioRef}
            src={selected.audioPath ? `file://${selected.audioPath}` : undefined}
            onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
            onEnded={() => setPlaying(false)}
            hidden
          />
        </footer>
      )}
    </div>
  );
}

const iSearch = <svg viewBox="0 0 24 24" className="ic"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.2-3.2" /></svg>;
const iFile = <svg viewBox="0 0 24 24" className="ic"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" /><path d="M14 3v5h5" /></svg>;
