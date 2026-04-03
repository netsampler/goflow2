# FlowStore

This document describes the generic `FlowStore` storage model, hook behavior, expiry model, and the relationships between the FlowStore-backed store layers.

## Overview

`FlowStore` is the generic storage primitive in `pkg/flowstore/store.go`. It provides a reusable in-memory key/value store with:

* generic keys and values
* `Set` and `Add` semantics
* optional TTL expiry
* optional refresh-on-read and refresh-on-write
* FIFO eviction
* hooks for set, get, and delete events
* lifecycle helpers such as sweepers, `Start()`, and `Close()`

`Set` is the main access path for the FlowStore-backed wrappers in this repository, including template access and sampling-rate storage. `Add` is intended for aggregated values such as counters, where a delta is merged into an existing entry instead of replacing it.

`FlowStore` itself is intentionally protocol-agnostic. It does not know anything about NetFlow, IPFIX, templates, or counters. It is only the storage engine.

In this repository, the two main uses of `FlowStore` are template access and flow counters. Template access is implemented in the template and sampling-rate stores and mainly relies on `Set`. Counter-style aggregation is demonstrated in `pkg/flowstore` with `FlowIPv4Key`, `FlowCounters`, and `FlowTimestamp`, where `Add` merges packet, byte, and timestamp updates into an existing flow entry.

## Expiry

FlowStore supports both manual and automatic expiration control.

Manual control:

* `ExpireStale()` removes expired entries immediately.
* `WithTTL(ttl)` sets a per-write TTL.
* `WithoutExpiration()` disables expiration for a specific entry.

Automatic control:

* `WithDefaultTTL(ttl)` sets the default TTL for new entries.
* `StartSweeper(interval)` or `Start(interval)` enables periodic expiry checks.

TTL extension modes:

* none
  Entries expire once their TTL elapses.
* on write
  `WithRefreshTTLOnWrite()` refreshes TTL on `Set` and `Add`.
* on read and write
  `WithRefreshTTLOnWrite()` together with `WithRefreshTTLOnRead()` refreshes TTL on both updates and `Get`.

Typical choices depend on the workload:

* Templates can safely extend on read and write. As long as the decoder keeps reading a template, keeping it in memory is usually the desired behavior.
* Counters often use no extension when they should reset periodically.
* Counters can also use write-only extension when active writes should keep them alive, but idle counters should still age out.
* For continuous counters, disable expiration entirely. Those counters can be dumped periodically through a separate hook or flush path instead of relying on TTL.

`FlowCounters` is only a base building block. It can be embedded into a richer value type and combined with additional fields such as strings, timestamps, identifiers, or other per-flow state, as long as the resulting value implements the behaviors needed by `Set` and `Add`.

## List Keys

FlowStore exposes `Range(...)` to iterate over all live entries in FIFO order. `Range` also prunes stale entries before iteration, so the traversal acts on the current in-memory view.

FlowStore can also limit the number of stored keys with `WithMaxSize(max)`. When the limit is exceeded, the oldest entries are evicted in FIFO order, and those removals are reported as `DeleteReasonEvicted`.

Higher-level stores build snapshot helpers on top of that:

* `(*TemplateFlowStore).GetAll()`
* `(*SamplingRateFlowStore).GetAll()`

For dump-style workflows, callers typically iterate or snapshot the store and then serialize the result. In the current application wiring, JSON serialization is owned by the shared persistence manager rather than by the stores themselves.

## Store Relationships

So the layering is:

* `pkg/flowstore.Store[K,V]`
  Generic storage engine
* `decoders/netflow.TemplateStore`
  Minimal decoder-facing template interface
* `decoders/netflow.ManagedTemplateStore`
  Lifecycle and operational template interface
* `utils/store/templates.TemplateFlowStore`
  Template-specific implementation backed by `FlowStore`
* `utils/store/samplingrate.SamplingRateFlowStore`
  Sampling-rate implementation backed by `FlowStore`
* `metrics.TemplateStoreHooks()` / `metrics.SamplingRateStoreHooks()`
  Prometheus hook adapters for template and sampling-rate stores
* `utils/store/persistence.Manager`
  Shared JSON preload, flush, and HTTP document manager

```mermaid
flowchart LR
    App[App]
    Decoder[Decoder]
    Producer[ProtoProducer]
    TS["decoders/netflow.TemplateStore"]
    MTS["decoders/netflow.ManagedTemplateStore"]
    TFS["utils/store/templates.TemplateFlowStore"]
    SFS["utils/store/samplingrate.SamplingRateFlowStore"]
    PM["utils/store/persistence.Manager"]
    MH["metrics.TemplateStoreHooks() / metrics.SamplingRateStoreHooks()"]
    FS["pkg/flowstore.Store[K,V]"]
    HTTP["/store HTTP endpoint"]
    MetricsHTTP["/metrics HTTP endpoint"]
    JSON["store.json file"]

    App -->|creates| PM
    App -->|creates| Decoder
    App -->|builds| Producer
    Decoder -->|uses| TS
    MTS -->|extends| TS
    TFS -->|implements| MTS
    TFS -->|uses| FS
    SFS -->|uses| FS
    PM -->|creates and preloads| TFS
    PM -->|creates and preloads| SFS
    PM -->|composes persistence hooks into| TFS
    PM -->|composes persistence hooks into| SFS
    MH -->|composes metrics hooks into| TFS
    MH -->|composes metrics hooks into| SFS
    PM -->|reads/writes| JSON
    HTTP -->|renders| PM
    MetricsHTTP -->|exports| MH
    Producer -->|uses| SFS
```

The decoder depends only on `TemplateStore`. The application wiring uses `TemplateFlowStore` and `SamplingRateFlowStore` directly. Prometheus integration is attached through composed store hooks rather than wrapper stores. The producer uses `SamplingRateFlowStore` to resolve sampling-rate state while encoding flows. JSON preload and flush are handled by `persistence.Manager`, which creates the template and sampling-rate stores, composes persistence hooks into them, and owns the shared `store.json` document and `/store` HTTP rendering.

## Hook Model

`FlowStore` defines four hook types:

* `OnSetMutate`
  Runs under the store lock after `Set` or `Add` mutates a value. It receives `*V` and may modify the stored value in place.
* `OnSet`
  Runs after the store lock is released for completed `Set` and `Add` operations.
* `OnGet`
  Runs after the store lock is released for `Get`. It is not fired by `GetQuiet`.
* `OnDelete`
  Runs after the store lock is released when an entry is removed explicitly, expired, evicted, or flushed.

## Operation Flow

### `Set` and `Add`

```mermaid
flowchart TD
    A[Start Set/Add] --> B[Lock store]
    B --> C[Lookup key]
    C --> D{Existing entry expired?}
    D -- No --> G[Apply Set/Add]
    D -- Yes --> E[Call ExpireHook]
    E --> F{Extended?}
    F -- Yes --> G
    F -- No --> F1[Queue OnDelete Expired and remove entry]
    F1 --> G
    G --> H[Call OnSetMutate under lock]
    H --> I[Apply TTL and max-size bookkeeping]
    I --> J[Notify change channel]
    J --> K[Queue OnSet]
    K --> L[Unlock store]
    L --> M[Fire queued hooks]
    M --> N[OnSet]
    M --> O[OnDelete for any expiry or eviction queued during write]
```

Behavior:

* `OnSetMutate` runs under lock and can change the stored value.
* `OnSet` runs after unlock and observes the completed write.
* If `WithMaxSize(max)` evicts older entries during the write, each eviction queues `OnDelete(..., DeleteReasonEvicted)`.

### `Get`

```mermaid
flowchart TD
    A[Start Get] --> B[Lock store]
    B --> C[Lookup key]
    C --> D{Expired?}
    D -- No --> H{Refresh TTL on read?}
    D -- Yes --> E[Call ExpireHook]
    E --> F{Extended?}
    F -- Yes --> H
    F -- No --> G[Queue OnDelete Expired and remove entry]
    G --> G1[Unlock store]
    G1 --> G2[Fire OnDelete]
    G2 --> Z[Return miss]
    H --> I[Queue OnGet]
    I --> J[Copy value]
    J --> K[Unlock store]
    K --> L[Fire OnGet]
    L --> Y[Return hit]
```

Behavior:

* `Get` may extend TTL if `WithRefreshTTLOnRead()` is enabled.
* `OnGet` fires only for `Get`, not for `GetQuiet`.
* If an expired entry is not extended, `OnDelete(..., DeleteReasonExpired)` is fired.

### `GetQuiet`

```mermaid
flowchart TD
    A[Start GetQuiet] --> B[Lock store]
    B --> C[Lookup key]
    C --> D{Expired?}
    D -- No --> G[Copy value]
    D -- Yes --> E[Call ExpireHook]
    E --> F{Extended?}
    F -- Yes --> G
    F -- No --> H[Remove entry]
    H --> I[Unlock store]
    I --> Z[Return miss]
    G --> J[Unlock store]
    J --> Y[Return hit]
```

Behavior:

* `GetQuiet` does not queue `OnGet`.
* `GetQuiet` does not refresh TTL on read.

### `Delete`

```mermaid
flowchart TD
    A[Start Delete] --> B[Lock store]
    B --> C[Lookup key]
    C --> D[Queue OnDelete Explicit]
    D --> E[Remove entry]
    E --> F[Notify change channel]
    F --> G[Unlock store]
    G --> H[Fire OnDelete]
```

### `ExpireStale` and Sweeper

```mermaid
flowchart TD
    A[Start ExpireStale or sweeper tick] --> B[Lock store]
    B --> C[Scan entries]
    C --> D{Expired?}
    D -- No --> C
    D -- Yes --> E[Call ExpireHook]
    E --> F{Extended?}
    F -- Yes --> C
    F -- No --> G[Queue OnDelete Expired and remove entry]
    G --> H[Notify change channel]
    H --> C
    C --> I[Unlock store]
    I --> J[Fire queued OnDelete hooks]
```

### `Flush` and `Close`

```mermaid
flowchart TD
    A[Start Flush or Close] --> B[Lock store]
    B --> C[Iterate entries]
    C --> D[Queue OnDelete Flushed and remove entry]
    D --> C
    C --> E[Notify change channel]
    E --> F[Unlock store]
    F --> G[Fire queued OnDelete hooks]
```

## Delete Reasons

`OnDelete` may be fired with:

* `DeleteReasonExplicit`
* `DeleteReasonExpired`
* `DeleteReasonEvicted`
* `DeleteReasonFlushed`

## Summary

* `OnSetMutate` changes stored values during `Set` and `Add`.
* `OnSet` observes completed writes.
* `OnGet` observes `Get`, but not `GetQuiet`.
* `OnDelete` reports explicit deletes, expiry, eviction, and flushes.
