const WRITE_BATCH_BYTES = 32 << 10;
const MAX_WRITES_IN_FLIGHT = 4;

type Output = { bytes: Uint8Array; seq?: number };
type Entry = Output | (() => void);

/** Feed xterm's time-sliced parser a few bounded writes, preserving VT event order. */
export function projectShellOutput(
  write: (bytes: Uint8Array, parsed: () => void) => void,
  applied: (seq: number) => void,
) {
  const entries: Entry[] = [];
  let next = 0;
  let inFlight = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let disposed = false;

  const schedule = () => {
    if (disposed || timer !== undefined || next >= entries.length || inFlight >= MAX_WRITES_IN_FLIGHT) return;
    if (inFlight && typeof entries[next] === 'function') return;
    timer = setTimeout(flush, 0);
  };

  function flush() {
    timer = undefined;
    while (!disposed && next < entries.length) {
      const entry = entries[next];
      if (typeof entry === 'function') {
        if (inFlight) break;
        next++;
        entry();
        continue;
      }
      if (inFlight >= MAX_WRITES_IN_FLIGHT) break;

      const start = next++;
      let length = entry.bytes.byteLength;
      let seq = entry.seq;
      while (next < entries.length) {
        const following = entries[next];
        if (typeof following === 'function' || length + following.bytes.byteLength > WRITE_BATCH_BYTES) break;
        length += following.bytes.byteLength;
        seq = following.seq ?? seq;
        next++;
      }

      let bytes = entry.bytes;
      if (next - start > 1) {
        bytes = new Uint8Array(length);
        let offset = 0;
        for (let i = start; i < next; i++) {
          const data = (entries[i] as Output).bytes;
          bytes.set(data, offset);
          offset += data.byteLength;
        }
      }
      inFlight++;
      write(bytes, () => {
        inFlight--;
        if (!disposed && seq !== undefined) applied(seq);
        schedule();
      });
    }
    // Keep the unsubmitted suffix without shifting the array for each frame.
    if (next) entries.splice(0, next);
    next = 0;
  }

  return {
    data(bytes: Uint8Array, seq: number) {
      if (disposed) return;
      // A snapshot or aggregated transport frame can itself exceed one parser slice.
      while (bytes.byteLength > WRITE_BATCH_BYTES) {
        entries.push({ bytes: bytes.subarray(0, WRITE_BATCH_BYTES) });
        bytes = bytes.subarray(WRITE_BATCH_BYTES);
      }
      entries.push({ bytes, seq });
      schedule();
    },
    barrier(apply: () => void) {
      if (disposed) return;
      entries.push(apply);
      schedule();
    },
    dispose() {
      disposed = true;
      clearTimeout(timer);
      entries.length = 0;
    },
  };
}
