package usecase
import (
 "context"
 "encoding/json"
 "github.com/davveo/lumo/internal/constants"
 "github.com/davveo/lumo/internal/fulfillmentplan"
 "github.com/davveo/lumo/internal/platform"
 "github.com/davveo/lumo/services/order/internal/domain"
 "github.com/davveo/lumo/services/order/internal/repository"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
)
func (s *Service) parcelProgress(ctx context.Context,e platform.Event)error{
 var in struct{Version int64 `json:"version"`;Lines []fulfillmentplan.Progress `json:"lines"`}
 if err:=platform.Decode(e.Data,&in);err!=nil{return err};if in.Version<1||len(in.Lines)<1||len(in.Lines)>100{return status.Error(codes.InvalidArgument,"无效履约进度")}
 _,err:=s.update(ctx,"parcel_progress",OrderRequest{Actor:Actor{UserID:e.UserID,Role:constants.RoleSystem},ID:platform.Text(e.Data,"order_id")},internalAccess,func(_ *repository.Tx,o *domain.Order)(transition,error){
  if in.Version<=o.FulfillmentVersion{return transition{unchanged:true},nil}
  var quote struct{Items []fulfillmentplan.Line `json:"items"`};if json.Unmarshal([]byte(o.Quote),&quote)!=nil{return transition{},status.Error(codes.Internal,"订单快照不可用")}
  expected:=map[string]int64{};for _,l:=range quote.Items{expected[l.SKUID]+=l.Quantity};if len(expected)!=len(in.Lines){return transition{},status.Error(codes.InvalidArgument,"履约进度商品行不匹配")}
  shipped,delivered,active:=int64(0),int64(0),int64(0);seen:=map[string]bool{}
  for _,l:=range in.Lines{if seen[l.SKUID]||expected[l.SKUID]!=l.Quantity||l.Cancelled<0||l.Shipped<0||l.Delivered<0||l.Delivered>l.Shipped||l.Cancelled+l.Shipped>l.Quantity{return transition{},status.Error(codes.InvalidArgument,"履约数量超出订单")};seen[l.SKUID]=true;shipped+=l.Shipped;delivered+=l.Delivered;active+=l.Quantity-l.Cancelled}
  raw,_:=json.Marshal(in.Lines);o.FulfillmentProgress=string(raw);o.FulfillmentVersion=in.Version
  // Completion is irreversible. An older shipment callback cannot reopen it.
  if o.Status==constants.OrderStatusCompleted{return transition{},nil}
  if o.Status!=constants.OrderStatusReadyToFulfill&&o.Status!=constants.OrderStatusPartiallyShipped&&o.Status!=constants.OrderStatusShipped{return transition{},status.Error(codes.FailedPrecondition,"订单尚未进入履约阶段")}
  o.Status=constants.OrderStatusReadyToFulfill
  if shipped>0{o.Status=constants.OrderStatusPartiallyShipped;if shipped==active{o.Status=constants.OrderStatusShipped};if delivered==active{o.Status=constants.OrderStatusCompleted;return transition{eventType:constants.EventOrderCompleted,data:completionData(o)},nil}}
  return transition{},nil
 });return err
}
