# aftersale-service

Owns customer applications, reviewed eligibility, claimed order-line quantity, physical return state and refund/exchange orchestration. It delegates money movement to refund and inventory movement to fulfillment/inventory.

Actions: `create`, `list`, `get`, staff `approve/reject`, owner `cancel/return`, staff `inspect`, and authorized workflow `refund/exchange`. Application creation requires an idempotency key and checks the order owner, paid/fulfillment state and immutable purchase snapshot. `lines` contains `{sku_id,quantity}`; omitted lines mean the whole order. Active/completed claims cannot exceed purchased quantities. A lock row serializes claims per order, including concurrent different application IDs.

The cash amount is allocated from original order cash payment; the last cash-return claim absorbs integer rounding. An application under review holds its quantities. Rejection/cancellation releases the quantity claim. Refunds stay REFUND_REQUESTED/REFUNDING until an actual `RefundSucceeded` event arrives; an RPC acceptance is not completion.

Return/refund: REQUESTED → APPROVED → RETURNING → INSPECTING → INSPECTED → REFUND_REQUESTED → REFUNDING → COMPLETED. Refund-only skips physical return but first requires confirmed fulfillment cancellation. Damaged or unsellable returns never increase sellable inventory.

Before shipment, refund-only applications must cover the whole order because warehouse cancellation releases the entire order allocation. Partial refund-only applications are rejected before creating a claim or cancelling fulfillment. Physical returns after shipment can still cover selected purchased lines.

Exchange: receive/inspect old goods → reserve and confirm replacement inventory → create a separate replacement fulfillment → EXCHANGING → replacement delivery → COMPLETED. The implementation accepts same-SKU or equal-price replacement; a price-changing replacement is explicitly rejected until a differential-payment workflow is configured. It does not grant a free upgrade or pretend a supplementary payment exists.

`Tick` resumes interrupted inspections, refund requests and exchange preparation. Deterministic receipt/refund/reservation IDs prevent duplicated effects. These workflows have explicit usecase/repository/transport spans and propagate the same context into RPC and GORM operations.
