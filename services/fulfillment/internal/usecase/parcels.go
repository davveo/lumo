package usecase
import (
 "context"
 "crypto/sha256"
"encoding/json"
 "fmt"
 "os"
 "github.com/davveo/lumo/internal/constants"
 "github.com/davveo/lumo/internal/fulfillmentplan"
 "github.com/davveo/lumo/internal/platform"
 "github.com/davveo/lumo/services/fulfillment/internal/domain"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
)
func operationID(prefix,order,key string)string{sum:=sha256.Sum256([]byte(order+"\x00"+key));return fmt.Sprintf("%s-%x",prefix,sum[:16])}
func (s *Service) orderRoot(ctx context.Context,c platform.Request)(*domain.Record,error){
 r,e:=s.Repo.Get(ctx,c.ID)
 if status.Code(e)==codes.NotFound{
  o,err:=platform.NewClients(s.RPC).Order.Get(ctx,platform.Request{ID:c.ID,UserID:c.UserID,Role:c.Role});if err!=nil{return nil,err}
  if platform.Text(o,"status")!=constants.OrderStatusReadyToFulfill{return nil,status.Error(codes.FailedPrecondition,"等待订单进入仓库后再处理商品行")}
  _,encoded,err:=fulfillmentplan.Normalize(o["items"]);if err!=nil{return nil,err}
  r=&domain.Record{ID:c.ID,OrderID:c.ID,UserID:platform.Text(o,"user_id"),ReservationID:c.ID,Kind:constants.FulfillmentKindOrder,Status:constants.FulfillmentStatusReady,LinesJSON:encoded};if err=s.Repo.Create(ctx,r);err!=nil{return nil,err};e=nil
 }
 if e!=nil{return nil,e};if e=owned(c,r);e!=nil{return nil,e};if c.Role==constants.RoleSystem&&c.UserID!=r.UserID{return nil,status.Error(codes.PermissionDenied,"订单账号不匹配")};if r.Kind!=constants.FulfillmentKindOrder{return nil,status.Error(codes.InvalidArgument,"普通订单编号必填")};return r,nil
}
func (s *Service) LineState(ctx context.Context,c platform.Request)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"fulfillment.usecase.LineState");defer sp.End();if e:=platform.RequireUser(c);e!=nil{return nil,e}
 root,e:=s.orderRoot(ctx,c);if e!=nil{return nil,e};st,e:=s.Repo.PlanState(ctx,root.ID);if e!=nil{return nil,e};out:=st.Result();out["order_id"]=root.OrderID;out["parcelized"]=root.Parcelized;return out,nil
}
func (s *Service) HoldLines(ctx context.Context,c platform.Request)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"fulfillment.usecase.HoldLines");defer sp.End();if c.Role!=constants.RoleSystem{return nil,status.Error(codes.PermissionDenied,"仅售后内部命令可拦截商品")}
 root,e:=s.orderRoot(ctx,c);if e!=nil{return nil,e};lines,encoded,e:=fulfillmentplan.Normalize(c.Data["lines"]);if e!=nil{return nil,e};key:=platform.Text(c.Data,"aftersale_id");if key==""||len(key)>120{return nil,status.Error(codes.InvalidArgument,"售后编号必填")}
 hold,e:=s.Repo.HoldLines(ctx,root.ID,operationID("cancel",root.ID,key),encoded,lines);return platform.Map(hold),e
}
func (s *Service) hold(ctx context.Context,c platform.Request)(*domain.Record,error){
 if c.Role!=constants.RoleSystem{return nil,status.Error(codes.PermissionDenied,"仅售后内部命令可处理拦截")};id:=platform.Text(c.Data,"aftersale_id");if id==""{return nil,status.Error(codes.InvalidArgument,"售后编号必填")};r,e:=s.Repo.Get(ctx,operationID("cancel",c.ID,id));if e!=nil{return nil,e};if r.UserID!=c.UserID||r.Kind!=constants.FulfillmentKindCancellation{return nil,status.Error(codes.PermissionDenied,"拦截账号不匹配")};return r,nil
}
func (s *Service) ReleaseHold(ctx context.Context,c platform.Request)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"fulfillment.usecase.ReleaseHold");defer sp.End();r,e:=s.hold(ctx,c);if e!=nil{return nil,e};if r.Status==constants.FulfillmentStatusCancelled{return nil,status.Error(codes.FailedPrecondition,"商品已完成取消")};r,e=s.Repo.ChangeHold(ctx,r,constants.FulfillmentStatusReleased);return platform.Map(r),e
}
func (s *Service) CancelLines(ctx context.Context,c platform.Request)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"fulfillment.usecase.CancelLines");defer sp.End();r,e:=s.hold(ctx,c);if e!=nil{return nil,e};return s.cancelHeld(ctx,r)
}
func (s *Service) cancelHeld(ctx context.Context,r *domain.Record)(platform.Result,error){
 if r.Status==constants.FulfillmentStatusCancelled{return platform.Result{"id":r.ID,"warehouse_confirmed":true},nil}
 var e error
 if r.Status==constants.FulfillmentStatusHeld{r,e=s.Repo.ChangeHold(ctx,r,constants.FulfillmentStatusCancelRequested);if e!=nil{return nil,e}}
 if r.Status!=constants.FulfillmentStatusCancelRequested&&r.Status!=constants.FulfillmentStatusWarehouseCancelConfirmed{return nil,status.Error(codes.FailedPrecondition,"拦截申请不可取消")}
 if r.Status==constants.FulfillmentStatusCancelRequested{if os.Getenv("DEMO_MODE")!="true"{return nil,status.Error(codes.FailedPrecondition,"等待已认证的仓库取消回执")};r,e=s.Repo.ChangeHold(ctx,r,constants.FulfillmentStatusWarehouseCancelConfirmed);if e!=nil{return nil,e}}
 ls,e:=fulfillmentplan.Parse(r.LinesJSON);if e!=nil{return nil,e}
 if _,e=platform.NewClients(s.RPC).Inventory.CancelLines(ctx,platform.Request{ID:r.ReservationID,UserID:r.UserID,Role:constants.RoleSystem,Data:map[string]any{"operation_id":r.ID,"lines":ls,"warehouse_confirmed":true}});e!=nil{return nil,e}
 r,e=s.Repo.ChangeHold(ctx,r,constants.FulfillmentStatusCancelled);out:=platform.Map(r);out["warehouse_confirmed"]=e==nil;return out,e
}
func (s *Service) ParcelCreate(ctx context.Context,c platform.Request)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"fulfillment.usecase.ParcelCreate");defer sp.End();if !internal(c){return nil,status.Error(codes.PermissionDenied,"需要仓库装包权限")};if c.IdempotencyKey==""||len(c.IdempotencyKey)>120{return nil,status.Error(codes.InvalidArgument,"装包请求编号必填")}
 root,e:=s.orderRoot(ctx,c);if e!=nil{return nil,e};ls,encoded,e:=fulfillmentplan.Normalize(c.Data["lines"]);if e!=nil{return nil,e};r,e:=s.Repo.CreateParcel(ctx,root.ID,operationID("parcel",root.ID,c.IdempotencyKey),encoded,ls);return platform.Map(r),e
}
func (s *Service) ParcelCancel(ctx context.Context,c platform.Request)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"fulfillment.usecase.ParcelCancel");defer sp.End();if !internal(c){return nil,status.Error(codes.PermissionDenied,"需要仓库装包权限")};r,e:=s.Repo.Get(ctx,c.ID);if e!=nil{return nil,e};if r.Kind!=constants.FulfillmentKindParcel{return nil,status.Error(codes.InvalidArgument,"包裹编号必填")};r,e=s.Repo.ChangeParcel(ctx,r,constants.FulfillmentStatusCancelled,"",nil);return platform.Map(r),e
}
func (s *Service) dispatchParcel(ctx context.Context,r *domain.Record)(platform.Result,error){
 if r.Status==constants.FulfillmentStatusDispatched||r.Status==constants.FulfillmentStatusDelivered{return platform.Map(r),nil}
 r,e:=s.Repo.ChangeParcel(ctx,r,constants.FulfillmentStatusDispatching,"",nil);if e!=nil{return nil,e}
 order,e:=platform.NewClients(s.RPC).Order.BeginDispatch(ctx,platform.Request{ID:r.OrderID,UserID:r.UserID,Role:constants.RoleSystem});if e!=nil{return nil,e}
 ls,e:=fulfillmentplan.Parse(r.LinesJSON);if e!=nil{return nil,e}
 if _,e=platform.NewClients(s.RPC).Inventory.DispatchLines(ctx,platform.Request{ID:r.ReservationID,UserID:r.UserID,Role:constants.RoleSystem,Data:map[string]any{"operation_id":r.ID,"lines":ls,"warehouse_confirmed":true}});e!=nil{return nil,e}
 address:=order["address"];addressJSON:="";if address!=nil{raw,_:=json.Marshal(address);addressJSON=string(raw)}
 shipping,e:=platform.NewClients(s.RPC).Logistics.Create(ctx,platform.Request{ID:r.ID,UserID:r.UserID,Role:constants.RoleSystem,Data:map[string]any{"order_id":r.OrderID,"kind":r.Kind,"address_json":addressJSON,"lines_json":r.LinesJSON}});if e!=nil{return nil,e};tracking:=platform.Text(shipping,"tracking_id")
 event:=&platform.Event{ID:r.ID+":dispatched",Type:constants.EventShipmentDispatched,AggregateID:r.ID,UserID:r.UserID,Data:map[string]any{"order_id":r.OrderID,"fulfillment_id":r.ID,"kind":r.Kind,"tracking_id":tracking,"lines":ls}}
 r,e=s.Repo.ChangeParcel(ctx,r,constants.FulfillmentStatusDispatched,tracking,event);return platform.Map(r),e
}
func (s *Service) deliveredParcel(ctx context.Context,e platform.Event)error{
 if platform.Text(e.Data,"kind")!=constants.FulfillmentKindParcel{return nil}
 id:=platform.Text(e.Data,"fulfillment_id");if id==""{id=e.AggregateID};r,err:=s.Repo.Get(ctx,id);if err!=nil{return err};if r.UserID!=e.UserID||r.Kind!=constants.FulfillmentKindParcel{return status.Error(codes.PermissionDenied,"签收包裹身份不匹配")};_,err=s.Repo.ChangeParcel(ctx,r,constants.FulfillmentStatusDelivered,"",nil);return err
}
