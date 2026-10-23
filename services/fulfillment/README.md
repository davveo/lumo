# fulfillment-service

Owns outbound warehouse jobs, cancellation acknowledgements and return inspection receipts. It never writes order or inventory tables.

Layers: `service.go` composes `transport → usecase → repository → GORM`; domain models and states are in `internal/domain`. All layer calls pass the tracing context. GORM SQL tracing is installed by the shared platform.

| Action | Caller | Meaning |
| --- | --- | --- |
| `list`, `get` | Owner or administrator | Paginated bounded job listing / one job |
| `dispatch` | Administrator or service | Persist DISPATCHING, call inventory.dispatch, create a logistics waybill, then persist DISPATCHED and its event |
| `cancel` | Service or administrator | Persist cancellation tombstone, warehouse cancellation request and acknowledgement, then release inventory allocation |
| `inspect_return` | Service or administrator | Fixed inspection ID, SKU, quantity and sellable/damaged/unsellable condition; independent inventory receipt |
| `create_exchange` | Service | Separate replacement fulfillment with its own reservation, linked to the original order |

`OrderReadyToFulfill` is the only normal-order creation event. `PaymentSucceeded` cannot create warehouse work. A cancellation that arrives before the ready event leaves a tombstone, so a delayed message cannot revive shipment. Once DISPATCHING or DISPATCHED, cancellation fails instead of returning stock incorrectly.

`Tick` resumes interrupted dispatch/cancellation jobs. Inventory and carrier commands use deterministic IDs, so retrying an unknown RPC result is safe.

The bundled warehouse is explicitly a deterministic local adapter and requires `DEMO_MODE=true` for dispatch/cancellation acknowledgement. With demo disabled, those actions wait/fail clearly until a real WMS adapter is supplied. Return inspection is an explicit authorized staff confirmation; it never claims to have queried a real warehouse automatically.
