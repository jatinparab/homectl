# Manager Memory Leak Analysis

**Observed:** ~450 MB RSS after ~1.5 hours of runtime on a 951 MB VPS, with 910 MB swap in use.  
**Primary driver:** per-request goroutine and channel leaks in `agent_link.go`, compounded by the 1 Hz WebSocket dashboard tick.

---

## Root Cause #1 — `peer.pending` channels never deleted (CRITICAL)

**File:** `manager/agent_link.go`  
**Lines:** `requestAgentUnary` (286–305), `streamAgent` (307–342), `attachAgentLink` read loop (101–114)

Every outbound agent call does:
```go
peer.pending[id] = make(chan common.LinkMessage, 256)
```

The only deletion paths are:
- The `attachAgentLink` read loop deletes the entry when it sees `msg.Done == true`
- A 10-second first-message timeout in `requestAgent` (line 277)
- A send-failure to the peer (line 259)

`requestAgentUnary` (line 286–305) receives the first message from the channel and returns — it **never** deletes the `pending` entry itself. It relies entirely on the read loop to delete it when `Done=true` arrives. If the agent sends `Done=false` (or crashes before sending `Done`), the entry leaks permanently.

`streamAgent`'s failure path (line 318–323) is worse: if `first.Status != 200` it returns immediately without ever deleting or draining the pending channel. Any subsequent messages from the agent pile into the 256-slot buffer and the entry stays until the peer disconnects.

When a peer disconnects, `attachAgentLink`'s defer (lines 86–93) deletes the peer from `m.agentLinks` but **never walks `peer.pending`** to close or delete the leaked channels.

**Why this is bad at 1 Hz:** The WebSocket tick calls `handleAPIContainers`, `handleAPIStorage`, `handleAPIApps` every second. Each fans out `requestAgentUnary` to every online agent. That's 2–4 outbound calls per agent per second. Over 1.5 hours = 10,000–20,000 calls per agent. Each leaked entry is a 256-slot `chan common.LinkMessage` (~4 KB header capacity).

**Proposed fix:**
```go
// requestAgentUnary — add deferred cleanup
func (m *Manager) requestAgentUnary(ctx context.Context, peer *agentPeer, req common.LinkMessage) (common.LinkMessage, error) {
    id := req.ID
    ch := make(chan common.LinkMessage, 256)
    peer.pendingMu.Lock()
    peer.pending[id] = ch
    peer.pendingMu.Unlock()

    // Always delete the pending entry when we return, regardless of Done flag
    defer func() {
        peer.pendingMu.Lock()
        delete(peer.pending, id)
        peer.pendingMu.Unlock()
        // Drain so the forwarder goroutine (issue #2) can exit
        for len(ch) > 0 {
            <-ch
        }
    }()
    // ... rest unchanged
}

// attachAgentLink defer — close all pending channels on disconnect
defer func() {
    m.agentLinksMu.Lock()
    delete(m.agentLinks, agentID)
    m.agentLinksMu.Unlock()
    // NEW: close all pending channels so callers and forwarders unblock
    peer.pendingMu.Lock()
    for id, ch := range peer.pending {
        close(ch)
        delete(peer.pending, id)
    }
    peer.pendingMu.Unlock()
}()
```

---

## Root Cause #2 — Forwarder goroutine never exits (CRITICAL, amplifies #1)

**File:** `manager/agent_link.go`  
**Lines:** 267–274 (`requestAgent` buffered-channel path)

For every call with `waitFirst > 0`, after the first message arrives, the code creates a second 256-slot channel and spawns a forwarding goroutine:
```go
buffered := make(chan common.LinkMessage, 256)
go func() {
    for msg := range ch { // ch is the peer.pending entry
        buffered <- msg
    }
}()
```

This goroutine ranges over `ch`. The read loop in `attachAgentLink` **never closes `ch`** — it only `delete`s the map entry. After deletion, `ch` is unreferenced from `pending` but the goroutine still blocks on `<-ch` forever. The goroutine leaks.

**Scale:** 1 forwarder goroutine per `requestAgentUnary` call. At 2–4 calls/agent/second × 1.5 hours = 10,000–20,000 leaked goroutines per agent. At 8 KB stack each = **80–160 MB** in goroutine stacks alone, plus the 256-slot `buffered` channels (~4 KB each) = another 40–80 MB.

**Proposed fix:** Close `ch` when the pending entry is deleted, so the `for msg := range ch` loop exits:
```go
// In attachAgentLink read loop, when Done=true:
peer.pendingMu.Lock()
ch, ok := peer.pending[msg.ID]
if ok {
    delete(peer.pending, msg.ID)
    close(ch)  // ADD THIS — lets the forwarder goroutine exit
}
peer.pendingMu.Unlock()
```

Same close in the disconnect defer from fix #1 above.

---

## Root Cause #3 — `/docker/sync` builds entire tarball in memory (HIGH)

**File:** `manager/agent_link.go`  
**Lines:** `invokeRawEndpoint` (207–231), `handleDockerSync` (338–408)

`invokeRawEndpoint` captures the HTTP handler's response body into an `httptest.ResponseRecorder`, then calls `io.ReadAll(res.Body)` to get `[]byte`. This means the **entire docker config tarball lives in memory** as a `[]byte` that is then JSON-encoded (base64'd, ~33% larger) and sent as a single `LinkMessage.Body`.

If that `LinkMessage` is stuck in a leaked `peer.pending` channel (issue #1), the tarball stays alive indefinitely. A 50 MB docker config dir becomes ~67 MB as base64 JSON body, potentially duplicated across both the `ch` and `buffered` channels.

**Proposed fix:** Stream the tarball in chunks as multiple `LinkMessage` frames with `Done=true` on the final frame. This removes the need to materialise the entire archive in memory at once.

---

## Root Cause #4 — WebSocket `writeCh` backlog (HIGH)

**File:** `manager/ws_api.go`  
**Lines:** 92 (`writeCh := make(chan any, 256)`), 105–112 (`safeSend`)

Each connected WebSocket dashboard client has a 256-slot `chan any`. The 1 Hz push goroutine calls `safeSend` which blocks if the channel is full (no `default` drop path — it only returns false on `ctx.Done()`). Each queued element is a `liveStateMessage` containing the full app list, agent list, and all container data for every agent.

If a browser tab is backgrounded or slow, the backlog can fill to 256 messages. At even 200 KB per message that's 50 MB per slow client pinned in queue.

The `lastHash` deduplication (line 204) only prevents queueing when state is identical, which is rare since `LastSeen` timestamps change every heartbeat tick.

**Proposed fix:** Replace the 256-deep FIFO with a 1-slot conflation queue:
```go
writeCh := make(chan any, 1)

func safeSend(ch chan any, msg any) bool {
    select {
    case ch <- msg:
    default:
        // Drop old, replace with latest
        select {
        case <-ch:
        default:
        }
        select {
        case ch <- msg:
        default:
        }
    }
    return true
}
```
The client always gets the latest state, never a backlog.

---

## Secondary Issues

### `QuarantinedOn` slice grows unboundedly (`reconcile.go:321`)
`app.QuarantinedOn = append(app.QuarantinedOn, oldAgent)` is called on every migration with no deduplication and no trim. Over days/weeks this grows per-app. Fix: dedupe before append and prune entries older than the healing window.

### `pruneState` / `pendingGlobalRestores` maps never delete dead entries
Entries for apps or agents that no longer exist stay for the process lifetime. Low memory impact but steady growers.

### `savePruneState` goroutine storm (`backup.go:412`, `api.go:447`)
`go m.savePruneState()` is fire-and-forget, spawned on every heartbeat that has a newer `LastPruneAt`. Under contention these queue up. Fix: single serialiser goroutine with a 1-slot trigger channel.

---

## Fix Priority

| Priority | Fix |
|---|---|
| 1 | Close `ch` when deleting from `peer.pending` (stops goroutine leak #2) |
| 2 | `defer delete(peer.pending, id)` in `requestAgentUnary` and `streamAgent` failure path (stops channel leak #1) |
| 3 | Walk and close all `peer.pending` entries in the disconnect defer (stops both #1 and #2 on peer death) |
| 4 | WebSocket conflation queue instead of 256-deep FIFO (stops #4) |
| 5 | Stream docker sync in chunks instead of one giant `[]byte` (stops #3) |
