// Native events and the periodic poll share one queue. Reads never overlap,
// so an older response cannot overwrite a more recent connection state.
export function serializeRefresh<T>(read: () => Promise<T>, apply: (state: T) => void) {
  let inFlight: Promise<void> | undefined
  let queued = false
  return function refresh(): Promise<void> {
    queued = true
    if (!inFlight) {
      inFlight = (async () => {
        do {
          queued = false
          apply(await read())
        } while (queued)
      })().finally(() => { inFlight = undefined })
    }
    return inFlight
  }
}
