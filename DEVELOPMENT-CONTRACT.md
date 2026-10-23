# Shared implementation contract (2026-10-22)

Module: `github.com/davveo/lumo`. All paths are under this repository. Go 1.26. GORM v2; gRPC actual transport with generated protobuf Command/Result. Each deployment starts exactly one named service and writes its own schema; no service imports another service's implementation. Cross-service calls ONLY through `platform.Caller`.

## Shared platform API (platform owner implements these exact signatures)

```go
package platform
type Command struct {
 Action string `json:"action"`; ID string `json:"id,omitempty"`
 UserID string `json:"user_id,omitempty"`; Role string `json:"role,omitempty"`
 IdempotencyKey string `json:"idempotency_key,omitempty"`
 Data map[string]any `json:"data,omitempty"`
}
type Result = map[string]any
type Caller interface { Call(context.Context, string, Command) (Result,error) }
type Dependencies struct { DB *gorm.DB; RPC Caller; Redis *redis.Client }
type Module struct {
 Name string
 Migrate func(context.Context) error
 Seed func(context.Context) error
 Handle func(context.Context,Command)(Result,error)
 Topics []string
 OnEvent func(context.Context,Event)error
 Tick func(context.Context)error
}
type Event struct {
 ID string `json:"id"`; Type string `json:"type"`
 AggregateID string `json:"aggregate_id"`; UserID string `json:"user_id"`
 Data map[string]any `json:"data"`
}
func Enqueue(context.Context,*gorm.DB,Event) error // same TX as business mutation; W3C carrier persisted
func Start(context.Context,string)(context.Context,trace.Span)
func ID(string) string
func Decode(map[string]any,any) error
func Map(any) Result
func Text(map[string]any,string) string
func Int(map[string]any,string) int64
func RequireUser(Command) error
func RequireAdmin(Command) error
```

Every public service package exports `func New(d platform.Dependencies) platform.Module`. Standard layout:

```
services/<name>/service.go                    // composition root only
services/<name>/internal/domain/model.go     // models, states, policies
services/<name>/internal/repository/...      // GORM only; WithContext(ctx), explicit repo span
services/<name>/internal/usecase/...         // domain rules, RPC orchestration, explicit usecase span
services/<name>/internal/transport/...       // action allowlist, payload validation, dispatch span
```

Every meaningful layer starts an OTel child span using platform.Start and passes returned ctx down. ALL GORM calls must use WithContext(ctx), including transactions and callbacks. gRPC handlers have span + transport span, then usecase, repository, GORM SQL span. Never trace raw password/token/PII or interpolate query parameter values. Use gRPC status codes for errors. No TODO, fake unconditional success, panic stubs, cross-schema writes, floating dependency versions, or package-level request contexts.

## Identity and auth

RPC is internal: shared service credential in metadata, no RPC host ports published. Gateway calls IAM `validate` with data.token, gets user_id/role, sets Command principal; never trusts user_id/role from HTTP. Admin-only action services must RequireAdmin; user resources must RequireUser and ownership. For tests caller may be in-memory routing to Module.Handle (production remains RPC).

Demo user IDs: `demo` user, `admin` administrator. Passwords `Lumo@2026`, `Admin@2026`, bcrypt hashed and only seeded in DEMO_MODE. IAM `login` data username/password -> token,user_id,role. `validate` data token -> user_id,role. `logout` token invalidates server session. Gateway Authorization Bearer.

## Inter-service action contract and payloads

Identifiers and monetary values: string IDs, int64 cents (not floats). SKU IDs seeded: `sku-phone` price699900, `sku-headphones`129900, `sku-watch`114900, `sku-coffee`39900. Warehouse `default`. Quantities positive, per-SKU max10 on user cart/order. Main response model fields use snake_case.

* catalog `list`/`get`/`upsert` admin; get ID=sku; -> id,name,price_cents,active,brand,category. list -> items.
* inventory `get` ID=sku -> sku_id,on_hand,reserved,allocated,available. `reserve` ID=reservation, data `{order_id,lines:[{sku_id,quantity}]}`. `confirm`/`cancel` ID=reservation. `dispatch` ID=reservation actual allocated outflow. `restock` ID=inspection, data `{sku_id,quantity,condition}`; only sellable restores sellable. Admin `adjust`.
* campaign `evaluate` data subtotal_cents,addon_sku_id -> discount_cents,eligible. `reserve` ID=order data activity_id,quantity; `confirm`/`cancel` ID=order. `list`.
* coupon `list`; `reserve` ID=order data coupon_id,subtotal_cents -> discount_cents; `confirm`/`cancel` ID=order; `refund` ID=refund data order_id (whole-order only initially). Seed coupon `coupon-demo-100` amount10000 min100000.
* points `get` -> available,frozen; `reserve` ID=order data amount; `confirm`/`cancel` ID=order; `refund` ID=refund data order_id; `reward` ID=order data amount; `checkin`. Seed5000; 1point = 1cent, at most10% original subtotal.
* cart `list`/`add` data sku_id,quantity; `remove` ID=row/sku; `clear`; `merge` idempotency_key and data.items. list -> items[{sku_id,quantity}].
* pricing `quote` data `{items:[{sku_id,quantity}],coupon_id,points,addon_sku_id,activity_id}` -> `{quote_id,subtotal_cents,discount_cents,points,payable_cents,items,coupon_id,activity_id}`; `get` ID=quote -> same + user_id,expires_at. Immutable server quote, validate stock/catalog on submit. Persist quote ownership.
* order `create` ID=order data quote(snapshot),address(snapshot),expires_at -> order. `pending` ID -> pending; `get`/`list`; internal `accept_payment` ID data payment_id,amount_cents -> accepted(bool); `ready` ID -> resources gate then ready; `close_begin`/`close_complete`; `shipped`/`complete`; `group_formed`/`group_failed`; `refunded` data refund_id,amount_cents. Payload get -> id,user_id,status,payable_cents,items,quote,coupon_id,points.
* checkout `quote` proxies pricing; `submit` idempotency_key data quote_id,address -> `{order_id,status}`. `paid` ID=order; `cancel` ID; OnEvent PaymentAccepted,OrderClosed,GroupFormed => commit/release. Persist Saga steps including UNKNOWN and cancel tombstones.
* payment `create` ID=order -> payment; `confirm_demo` ID=payment (DEMO_MODE/admin only) -> channel-style verified receipt; `get`; internal `close` ID=order freezes future attempts. Emits PaymentSucceeded. PaymentSucceeded consumer order -> accepts/rejects -> emits OrderPaymentAccepted or PaymentRejected. Decline goes refund.
* refund `create` ID=refund data order_id,payment_id,amount_cents,reason; `get`; `confirm_demo` ID admin; processing amount cap includes all pending+success. Emits RefundSucceeded. Reconciliation `list`/`run` compares only via RPC; no direct peer DB.
* group `list`/`create` data sku_id,required; `get`; `reserve` ID=order data group_id; `paid` ID=order; `cancel` ID=order; `expire`. Emits GroupFormed/GroupFailed. Connect checkout group_id via quote/submit if present.
* flash `list`/`request` idempotency_key data quote_id ->202-style request; `get` ID; Outbox FlashOrderRequested handler calls checkout.submit and stores result. Redis optional admission with durable database source.
* fulfillment `list`/`get`; `dispatch` ID=order admin -> inventory.dispatch+logistics.create; `cancel` ID=order -> warehouse cancellation confirmation then resource release allowed; `inspect_return` ID=aftersale data sku_id,quantity,condition -> inventory.restock. Consumes OrderReadyToFulfill.
* logistics `quote` -> shipping_cents=0 deterministic demo; `create` ID=order data order_id -> tracking; `get`; `deliver` ID admin -> DeliveryConfirmed.
* aftersale `create` idempotency data order_id,type(reason refund/return/exchange),reason -> aftersale; `list`/`get`; admin `approve`/`reject`; owner `return` tracking; admin `inspect` data condition; execute refund only after authorized state. Own rows quantity caps. `refund`/`exchange` transitions through RPC. RefundSucceeded closes own money state.
* invoice `create` data order_id,title,kind,tax_id -> request; `list`/`get`; admin `issue_demo` -> ISSUED; refund event -> reversal record, retain original. Do not delete issued invoice.
* member `get`/`update`; address `list`/`create`/`update`/`delete`/`default`, ownership. media `create` metadata only / `get` (no pretend object storage upload); engagement `list`/`put`/`delete` kind favorite/compare/history; interaction `list`/`create`/`like`/admin `moderate` (review requires completed order); content `list` public/admin `upsert`; analytics `record` idempotency event; notification `list`/`read`/`read_all` and event projection; helpdesk `list`/`create`/`reply`/`close`, ownership and staff role; search `query` via catalog + inventoryRPC (SQL read model first, no pretend ES implementation); recommendation `list` via catalog + inventory, exclude seed order items by RPC.

Events: exact PascalCase `OrderAwaitingPayment`, `PaymentSucceeded`, `OrderPaymentAccepted`, `PaymentRejected`, `OrderReadyToFulfill`, `OrderClosed`, `GroupFormed`, `GroupFailed`, `ShipmentDispatched`, `DeliveryConfirmed`, `OrderCompleted`, `RefundSucceeded`, `InvoiceIssued`, `TicketReplied`, `ProductChanged`, `FlashOrderRequested`. Event.Data includes IDs and relevant snapshot; downstream cannot infer fake user identity. Persist event.UserID. Never directly call a different service DB.

## Runtime signature (platform owner)

`OpenDB(ctx, driver, dsn string)(*gorm.DB,error)` driver mysql/sqlite; `InitTelemetry(ctx,service,endpoint string)(func(context.Context)error,error)` endpoint blank=>SDK without exporter (test hook for tracetest); `NewCaller(targets map[string]string, token string) Caller`; `ServeRPC(ctx,addr,token string,m Module)error`; `RunEvents(ctx,db,*redis.Client,url string,m Module)error`; `MigratePlatform(ctx,db)error`. Root may wrap startup separately; do not own cmd or registry.
