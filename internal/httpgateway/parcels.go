package httpgateway

func init() {
	adminActions["fulfillment/deliver"] = true
	userActions["fulfillment/line_state"] = true
	reads["line_state"] = true
	adminActions["fulfillment/parcel_create"] = true
	adminActions["fulfillment/parcel_cancel"] = true
}
