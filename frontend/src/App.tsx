import { useCallback, useEffect, useState } from "react";
import { api, Recording, Segment, SearchHit } from "./api";

// Minimal functional shell — NOT final design. The designer-grade pass goes
// through /design-studio + Edvin's eye later. This proves the golden path:
// import -> transcribe (stub) -> list -> view transcript -> search.

function formatTime(sec: number): string {
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
  const [importPath, setImportPath] = useState("");

  const refresh = useCallback(async () => {
    try {
      setRecordings(await api.listRecordings());
    } catch (e) {
      setError(String(e));
    }
  }, []);

  useEffect(() => {
    void refresh();
    api.engineName().then(setEngine).catch(() => setEngine("unknown"));
  }, [refresh]);

  const openRecording = useCallback(async (rec: Recording) => {
    setSelected(rec);
    setHits(null);
    try {
      setSegments(await api.getSegments(rec.id));
    } catch (e) {
      setError(String(e));
    }
  }, []);

  const doImport = useCallback(async () => {
    const path = importPath.trim();
    if (!path) return;
    setError(null);
    try {
      const rec = await api.importAndTranscribe(path, "");
      setImportPath("");
      await refresh();
      await openRecording(rec);
    } catch (e) {
      setError(String(e));
    }
  }, [importPath, refresh, openRecording]);

  const doSearch = useCallback(async () => {
    const q = query.trim();
    if (!q) {
      setHits(null);
      return;
    }
    try {
      setHits(await api.search(q));
    } catch (e) {
      setError(String(e));
    }
  }, [query]);

  const doDelete = useCallback(
    async (rec: Recording) => {
      try {
        await api.deleteRecording(rec.id);
        if (selected?.id === rec.id) {
          setSelected(null);
          setSegments([]);
        }
        await refresh();
      } catch (e) {
        setError(String(e));
      }
    },
    [selected, refresh],
  );

  return (
    <div className="app">
      <header className="topbar">
        <h1>Scribe</h1>
        <span className="engine">engine: {engine || "…"}</span>
      </header>

      {error && (
        <div className="error" role="alert">
          {error}
        </div>
      )}

      <div className="toolbar">
        <input
          className="input"
          placeholder="Path to audio file (import is a stub transcribe today)"
          value={importPath}
          onChange={(e) => setImportPath(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && void doImport()}
        />
        <button onClick={() => void doImport()}>Import &amp; transcribe</button>
      </div>

      <div className="toolbar">
        <input
          className="input"
          placeholder="Search transcripts…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && void doSearch()}
        />
        <button onClick={() => void doSearch()}>Search</button>
        {hits !== null && (
          <button
            className="ghost"
            onClick={() => {
              setHits(null);
              setQuery("");
            }}
          >
            Clear
          </button>
        )}
      </div>

      <main className="panes">
        <aside className="list">
          <h2>Recordings</h2>
          {recordings.length === 0 && <p className="muted">No recordings yet.</p>}
          <ul>
            {recordings.map((rec) => (
              <li
                key={rec.id}
                className={selected?.id === rec.id ? "active" : ""}
                onClick={() => void openRecording(rec)}
              >
                <div className="rec-title">{rec.title}</div>
                <div className="rec-meta">
                  {formatTime(rec.durationSec)}
                  <button
                    className="del"
                    onClick={(e) => {
                      e.stopPropagation();
                      void doDelete(rec);
                    }}
                    aria-label={`Delete ${rec.title}`}
                  >
                    ✕
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </aside>

        <section className="detail">
          {hits !== null ? (
            <>
              <h2>Search results ({hits.length})</h2>
              {hits.length === 0 && <p className="muted">No matches.</p>}
              <ul className="hits">
                {hits.map((h) => (
                  <li key={h.segmentId}>
                    <span className="ts">[{formatTime(h.start)}]</span>{" "}
                    <span className="hit-src">{h.recordingTitle}</span>
                    <div className="hit-text">{h.text}</div>
                  </li>
                ))}
              </ul>
            </>
          ) : selected ? (
            <>
              <h2>{selected.title}</h2>
              <ul className="transcript">
                {segments.map((seg) => (
                  <li key={seg.id}>
                    <span className="ts">
                      [{formatTime(seg.start)}–{formatTime(seg.end)}]
                    </span>
                    <span className="seg-text">{seg.text}</span>
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <p className="muted">Select a recording to view its transcript.</p>
          )}
        </section>
      </main>
    </div>
  );
}
