# Templates: Registry vs System

This document explains the template abstractions used by the NetFlow/IPFIX pipeline, common wrappers (pruning, expiry, persistence),
and guidance for adding a new persistence layer (e.g., Redis, S3).

## Concepts

* Template system: stores templates for a single router/source.
  * Interface: `decoders/netflow.NetFlowTemplateSystem`
  * Operations: add/get/remove templates, fetch all templates.
* Registry: maps a router/source key to a template system.
  * Interface: `utils/templates.Registry`
  * Responsibilities: create per-router systems, enumerate templates across routers, close resources.

The decoder is not aware of the "router" and uses the template system.
The pipe system (handles packet processing) uses the registry to provide the decoder with the correct template system.

    +------------------------+     +------------------------+
    |      Registry A        | --> |      Registry B        |
    | (JSON, Expiring, etc.) |     | (InMemory, etc.)       |
    |                        |     |                        |
    |  +------------------+  |     |  +------------------+  |
    |  | TemplateSystem A |  | --> |  | TemplateSystem B |  |
    |  | (wrappers, etc.) |  |     |  | (Basic store)    |  |
    |  +------------------+  |     |  +------------------+  |
    +------------------------+     +------------------------+

Registries and template systems can be chained, with the caveat that template removals aren't propagated to wrapping systems.
Expiration and loading should be handled at the top-level wrapper.

## Base storage

* `decoders/netflow.BasicTemplateSystem` keeps templates in a map keyed by version/obs-domain/template ID.
* `utils/templates.InMemoryRegistry` manages per-router systems using a generator (default uses `BasicTemplateSystem`).

## Common wrappers

* Pruning wrapper (`utils/templates.pruningTemplateSystem`)
  * Keeps a template count per router and triggers `onEmpty(key)` when the last template is removed.
  * Used by `InMemoryRegistry` and `JSONRegistry` to drop empty router entries.
* Expiry wrapper (`utils/templates.ExpiringTemplateSystem` / `ExpiringRegistry`)
  * Tracks template update timestamps and expires stale templates on a TTL.
  * Maintains per-router counts in the registry and prunes empty routers.
* Persistence wrapper (`utils/templates.JSONRegistry` / `jsonPersistingTemplateSystem`)
  * Persists all templates to a JSON file, batching writes.
  * Uses a pruning wrapper around the base system to maintain counts and delete empty routers.
* Metrics wrapper (`metrics.PromTemplateSystem` / `PromTemplateRegistry`)
  * Records Prometheus metrics on add/remove, delegates storage to wrapped system.

## Adding a new persistence layer (e.g., Redis)

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
  * If the registry needs to drop empty routers, wrap the base system with `pruningTemplateSystem` and provide `onEmpty(key)` to delete registry entries.
  * Call `initCountFromTemplates()` to seed counts when the wrapped system already has templates.
* Snapshot consistency:
  * Use `GetAll()` or `GetTemplates()` to snapshot templates for persistence.
  * Ensure your persistence layer handles concurrent updates (lock or batch as needed).
* Load order:
  * If you preload templates, do it before starting write-behind workers, so chains can initialize in order.
  * Loading should happen at the top-level wrapper (like JSON loading going through the expiring registry), not in inner wrappers.
