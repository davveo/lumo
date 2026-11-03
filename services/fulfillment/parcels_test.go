package fulfillment

import (
 "context"
 "encoding/json"
 "fmt"
 "path/filepath"
 "testing"
 "time"
 "github.com/davveo/lumo/internal/constants"
 "github.com/davveo/lumo/internal/platform"
 "github.com/davveo/lumo/services/inventory"
 "github.com/davveo/lumo/services/aftersale"
 ordermod "github.com/davveo/lumo/services/order"
 "github.com/davveo/lumo/services/logistics"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
 "gorm.io/gorm"
)
type parcelBus struct{modules map[string]platform.Module;dbs map[string]*gorm.DB;refunds int;failCancel bool}
func (b *parcelBus) Call(ctx context.Context,svc string,c platform.Command)(platform.Result,error){
 if svc=="refund"{b.refunds++;return platform.Result{"id":c.ID},nil}
 if svc=="inventory"&&c.Action=="cancel_lines"&&b.failCancel {b.failCancel=false;return nil,status.Error(codes.Unavailable,"temporary inventory failure")}
 m,ok:=b.modules[svc];if !ok{return nil,fmt.Errorf("unexpected %s/%s",svc,c.Action)};return m.Handle(ctx,c)
}
func parcelSetup(t *testing.T)*parcelBus{
 t.Helper();t.Setenv("DEMO_MODE","true");b:=&parcelBus{modules:map[string]platform.Module{},dbs:map[string]*gorm.DB{}}
 for name,newModule:=range map[string]func(platform.Dependencies)platform.Module{"fulfillment":New,"inventory":inventory.New,"order":ordermod.New,"aftersale":aftersale.New,"logistics":logistics.New}{
  db,e:=platform.OpenDB(context.Background(),"sqlite",filepath.Join(t.TempDir(),name+".db"));if e!=nil{t.Fatal(e)};conn,_:=db.DB();t.Cleanup(func(){_ = conn.Close()});if e=platform.MigratePlatform(context.Background(),db);e!=nil{t.Fatal(e)};m:=newModule(platform.Dependencies{DB:db,RPC:b});if e=m.Migrate(context.Background());e!=nil{t.Fatal(e)};b.modules[name]=m;b.dbs[name]=db
 };return b
}
func (b *parcelBus) call(t *testing.T,svc,action,id,user,role string,data map[string]any,key ...string)platform.Result{t.Helper();c:=platform.Command{Action:action,ID:id,UserID:user,Role:role,Data:data};if len(key)>0{c.IdempotencyKey=key[0]};r,e:=b.Call(context.Background(),svc,c);if e!=nil{t.Fatalf("%s/%s: %v",svc,action,e)};return r}
func (b *parcelBus) project(t *testing.T){t.Helper();for _,source:=range []string{"fulfillment","logistics"}{var records []platform.Outbox;if e:=b.dbs[source].Order("created_at,id").Find(&records).Error;e!=nil{t.Fatal(e)};for _,r:=range records{var event platform.Event;if e:=json.Unmarshal([]byte(r.PayloadJSON),&event);e!=nil{t.Fatal(e)};for _,target:=range []string{"fulfillment","order"}{m:=b.modules[target];for _,topic:=range m.Topics{if topic==event.Type{if e:=m.OnEvent(context.Background(),event);e!=nil{t.Fatalf("%s event %s: %v",target,event.Type,e)}}}}}}}
func (b *parcelBus) ready(t *testing.T,id string){
 items:=[]map[string]any{{"sku_id":"coffee","quantity":3,"unit_price_cents":100},{"sku_id":"tea","quantity":1,"unit_price_cents":200}}
 b.call(t,"order","create",id,"demo","system",map[string]any{"quote":map[string]any{"id":"q","items":items,"payable_cents":int64(401)},"address":map[string]any{"recipient":"成员","phone":"13800000000","address":"测试地址"},"expires_at":time.Now().Add(time.Hour).Format(time.RFC3339Nano)})
 b.call(t,"order","pending",id,"demo","system",nil);b.call(t,"order","accept_payment",id,"demo","system",map[string]any{"payment_id":"pay","amount_cents":401,"paid_at":time.Now().Format(time.RFC3339Nano)})
 b.call(t,"order","ready",id,"demo","system",nil)
 for _,sku:=range []string{"coffee","tea"}{b.call(t,"inventory","adjust",sku,"admin","admin",map[string]any{"on_hand":20,"reason":"initial"})}
 b.call(t,"inventory","reserve",id,"demo","system",map[string]any{"order_id":id,"lines":items});b.call(t,"inventory","confirm",id,"demo","system",nil)
 if e:=b.modules["fulfillment"].OnEvent(context.Background(),platform.Event{Type:constants.EventOrderReadyToFulfill,AggregateID:id,UserID:"demo",Data:map[string]any{"order_id":id,"items":items}});e!=nil{t.Fatal(e)}
}
func TestParcelCancellationStockAndDeliveryRemainConsistent(t *testing.T){
 b:=parcelSetup(t);b.ready(t,"split")
 line:=func(sku string,n int)[]map[string]any{return []map[string]any{{"sku_id":sku,"quantity":n}}}
 // An existing packing plan is invalidated when the same item enters review.
 old:=b.call(t,"fulfillment","parcel_create","split","admin","admin",map[string]any{"lines":line("coffee",3)},"old")
 claim:=b.call(t,"aftersale","create","","demo","user",map[string]any{"order_id":"split","type":"refund","reason":"cancel one","lines":line("coffee",1)},"cancel-one")
 cid:=platform.Text(claim,"id");if platform.Int(claim,"amount_cents")!=80{t.Fatal("allocated refund",claim)}
 b.call(t,"aftersale","approve",cid,"admin","admin",nil)
 _,e:=b.Call(context.Background(),"fulfillment",platform.Command{Action:"dispatch",ID:platform.Text(old,"id"),UserID:"admin",Role:"admin"});expectCode(t,e,codes.FailedPrecondition)
 b.failCancel=true
 _,e=b.Call(context.Background(),"aftersale",platform.Command{Action:"refund",ID:cid,UserID:"demo",Role:"user"});expectCode(t,e,codes.Unavailable);if b.refunds!=0{t.Fatal("cash moved before warehouse acknowledgement")}
 b.call(t,"aftersale","refund",cid,"demo","user",nil);b.call(t,"aftersale","refund",cid,"demo","user",nil)
 if b.refunds!=1{t.Fatal("duplicate refund",b.refunds)}
 stock:=b.call(t,"inventory","get","coffee","demo","user",nil);if platform.Int(stock,"allocated")!=2||platform.Int(stock,"on_hand")!=20{t.Fatal("wrong partial release",stock)}
 p1:=b.call(t,"fulfillment","parcel_create","split","admin","admin",map[string]any{"lines":line("coffee",2)},"one")
 p2:=b.call(t,"fulfillment","parcel_create","split","admin","admin",map[string]any{"lines":line("tea",1)},"two")
 b.call(t,"fulfillment","dispatch",platform.Text(p1,"id"),"admin","admin",nil);b.call(t,"fulfillment","dispatch",platform.Text(p1,"id"),"admin","admin",nil);b.project(t)
 order:=b.call(t,"order","get","split","demo","user",nil);if platform.Text(order,"status")!=constants.OrderStatusPartiallyShipped{t.Fatal(order)}
 _,e=b.Call(context.Background(),"order",platform.Command{Action:"complete",ID:"split",UserID:"demo",Role:"user"});expectCode(t,e,codes.FailedPrecondition)
 b.call(t,"logistics","deliver",platform.Text(p1,"id"),"admin","admin",nil);b.project(t)
 order=b.call(t,"order","get","split","demo","user",nil);if platform.Text(order,"status")==constants.OrderStatusCompleted{t.Fatal("first parcel completed entire order")}
 b.call(t,"fulfillment","dispatch",platform.Text(p2,"id"),"admin","admin",nil);b.project(t)
 b.call(t,"logistics","deliver",platform.Text(p2,"id"),"admin","admin",nil);b.project(t);b.project(t)
 order=b.call(t,"order","get","split","demo","user",nil);if platform.Text(order,"status")!=constants.OrderStatusCompleted||platform.Int(order,"payable_cents")!=401{t.Fatal("wrong final order",order)}
 stock=b.call(t,"inventory","get","coffee","demo","user",nil);if platform.Int(stock,"allocated")!=0||platform.Int(stock,"on_hand")!=18{t.Fatal("dispatch changed cancelled quantity",stock)}
 // A returned unit must come from the actually dispatched pool.
 _,e=b.Call(context.Background(),"aftersale",platform.Command{Action:"create",UserID:"demo",Role:"user",IdempotencyKey:"too-many",Data:map[string]any{"order_id":"split","type":"return","reason":"return","lines":line("coffee",3)}});expectCode(t,e,codes.FailedPrecondition)
 var completed int64;b.dbs["order"].Model(&platform.Outbox{}).Where("event_type = ?",constants.EventOrderCompleted).Count(&completed);if completed!=1{t.Fatal("completion event duplicated",completed)}
}
