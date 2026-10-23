# logistics-service

Owns waybills and delivery status. `create` is an internal fulfillment command with one deterministic waybill per fulfillment ID; `get/list` enforce user ownership; `deliver` is an authorized carrier/admin confirmation and atomically writes `DeliveryConfirmed` to Outbox. Repeated delivery confirmation emits no duplicate business event.

`quote` currently uses an explicit free-shipping demonstration policy (`shipping_cents=0`, provider `demo-free-shipping`). `create` uses `local-demo-carrier` only when `DEMO_MODE=true`; no real carrier account, physical shipment or external tracking is implied. Replace the carrier adapter and authenticated callback route before connecting a commercial carrier.

Replacement shipments carry `kind=EXCHANGE` and a separate `fulfillment_id`, while retaining the original `order_id`. Order lifecycle consumers must ignore replacement delivery when projecting the original sale; aftersale closes the linked exchange only when the replacement is delivered.

Directory layers are `internal/transport`, `internal/usecase`, `internal/repository` and `internal/domain`. Each layer and GORM operation retains the request context and corresponding trace child spans.
