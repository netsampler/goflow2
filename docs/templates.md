# Templates: Registry vs System

This document explains the template abstractions used by the NetFlow/IPFIX pipeline, common wrappers (pruning, expiry, persistence),
and guidance for adding a new persistence layer (e.g., Redis, S3).

## Concepts

* Template system: stores templates for a single router/source.
  * Interface: `decoders/netflow.NetFlowTemplateSystem`
  * Operations: add/get/remove templates, fetch all templates.
* Registry: maps a router/source key to a template system.
  * Interface: `utils/templates.Registry`
  * Responsibilities: create per-router systems, enumerate templates across routers, start background work, close resources.
  * Close should stop background work and flush/persist any pending state.

The decoder is not aware of the "router" and uses the template system.
The pipe system (handles packet processing) uses the registry to provide the decoder with the correct template system and calls `Start()` on the pipe to initialize registry background work.

    +------------------------+     +------------------------+
    |      Registry A        | --> |      Registry B        |
    | (JSON, Expiring, etc.) |     | (InMemory, etc.)       |
    |                        |     |                        |
    |  +------------------+  |     |  +------------------+  |
    |  | TemplateSystem A |  | --> |  | TemplateSystem B |  |
    |  | (wrappers, etc.) |  |     |  | (Basic store)    |  |
    |  +------------------+  |     |  +------------------+  |
    +------------------------+     +------------------------+

Registries and template systems can be chained, with the caveat that changes only flow inward (wrappers see inner changes) and removals are not propagated outward to wrapping systems.
Expiration and loading should be handled at the top-level wrapper.

## Base storage

* `decoders/netflow.BasicTemplateSystem` keeps templates in a map keyed by version/obs-domain/template ID.
* `utils/templates.InMemoryRegistry` manages per-router systems using a generator (default uses `BasicTemplateSystem`).

## Common wrappers

* Expiry wrapper (`utils/templates.ExpiringTemplateSystem` / `ExpiringRegistry`)
  * Tracks template update timestamps and expires stale templates on a TTL.
  * Maintains per-router counts in the registry and prunes empty routers (including empty systems that never received templates).
  * Used as the top-level registry by default; a TTL of 0 disables template expiry but still allows empty-system cleanup.
* Persistence wrapper (`utils/templates.JSONRegistry` / `jsonPersistingTemplateSystem`)
  * Persists all templates to a JSON file, batching writes.
  * Starts a background flush loop via `Start()`.
* Metrics wrapper (`metrics.PromTemplateSystem` / `PromTemplateRegistry`)
  * Records Prometheus metrics on add/remove, delegates storage to wrapped system.

## Adding a new persistence layer

### Wrap the registry (recommended)

* Implement a `Registry` that:
  * Wraps an existing registry (usually `NewInMemoryRegistry(nil)`).
  * On `GetSystem(key)`, returns a wrapped `NetFlowTemplateSystem` that:
    * Delegates storage to the wrapped system.
    * Notifies the registry of changes for persistence.
  * On persistence flush, uses `wrapped.GetAll()` to snapshot data.
  * Optionally supports preload (read templates from Redis and call `AddTemplate` on wrapped systems).

### Operational considerations

* Pruning:
  * Empty router pruning is handled by the expiring registry using the same TTL rules.
  * Inner registries/systems should not prune on their own; rely on `ExpiringRegistry` and its sweeper.
* Snapshot consistency:
  * Use `GetAll()` or `GetTemplates()` to snapshot templates for persistence.
  * Ensure your persistence layer handles concurrent updates (lock or batch as needed).
* Load order:
  * If you preload templates, do it before calling `Start()`, so chains can initialize in order.
