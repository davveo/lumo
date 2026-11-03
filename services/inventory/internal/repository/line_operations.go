package repository

import (
 "context"
 "encoding/json"
 "errors"
 "github.com/davveo/lumo/internal/constants"
 "github.com/davveo/lumo/internal/fulfillmentplan"
 "github.com/davveo/lumo/internal/platform"
 "github.com/davveo/lumo/services/inventory/internal/domain"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
 "gorm.io/gorm"
 "gorm.io/gorm/clause"
)

func (r *Repository) ApplyLines(ctx context.Context,c platform.Request,kind string)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"inventory.repository.ApplyLines");defer sp.End()
 lines,encoded,e:=fulfillmentplan.Normalize(c.Data["lines"]);if e!=nil{return nil,e}
 opID:=platform.Text(c.Data,"operation_id")
 if opID==""||len(opID)>120||c.ID==""{return nil,status.Error(codes.InvalidArgument,"预留编号和行操作编号必填")}
 if yes,_:=c.Data["warehouse_confirmed"].(bool);!yes{return nil,status.Error(codes.FailedPrecondition,"仓库确认后才可释放或扣减库存")}
 var reservation domain.Reservation
 e=r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB)error{
  if e:=tx.WithContext(ctx).Clauses(clause.Locking{Strength:"UPDATE"}).First(&reservation,"id = ?",c.ID).Error;e!=nil{return e}
  if reservation.UserID!=c.UserID{return status.Error(codes.PermissionDenied,"预留账号不匹配")}
  var old domain.LineOperation
  err:=tx.WithContext(ctx).First(&old,"id = ?",opID).Error
  if err==nil{if old.ReservationID!=c.ID||old.Kind!=kind||old.LinesJSON!=encoded{return status.Error(codes.AlreadyExists,"行操作编号已用于不同请求")};return nil};if !errors.Is(err,gorm.ErrRecordNotFound){return err}
  if reservation.State!=constants.InventoryReservationStateAllocated{return status.Error(codes.FailedPrecondition,"库存必须已分配且尚有剩余数量")}
  original,e:=fulfillmentplan.Parse(reservation.LinesJSON);if e!=nil{return e};remaining:=map[string]int64{};for _,l:=range original{remaining[l.SKUID]=l.Quantity}
  var previous []domain.LineOperation
  if e=tx.WithContext(ctx).Where("reservation_id = ?",c.ID).Find(&previous).Error;e!=nil{return e}
  dispatched:=int64(0)
  for _,o:=range previous{var ls []fulfillmentplan.Line;if e=json.Unmarshal([]byte(o.LinesJSON),&ls);e!=nil{return e};for _,l:=range ls{remaining[l.SKUID]-=l.Quantity;if o.Kind==constants.InventoryMovementKindDispatched{dispatched+=l.Quantity}}}
  for _,l:=range lines{
   if remaining[l.SKUID]<l.Quantity{return status.Error(codes.FailedPrecondition,"所选商品已发货或已取消")};remaining[l.SKUID]-=l.Quantity
   fields:=map[string]any{"allocated":gorm.Expr("allocated - ?",l.Quantity)};q:=tx.WithContext(ctx).Model(&domain.Stock{}).Where("sku_id = ? AND allocated >= ?",l.SKUID,l.Quantity)
   if kind==constants.InventoryMovementKindDispatched{q=q.Where("on_hand >= ?",l.Quantity);fields["on_hand"]=gorm.Expr("on_hand - ?",l.Quantity);dispatched+=l.Quantity}
   result:=q.Updates(fields);if result.Error!=nil{return result.Error};if result.RowsAffected!=1{return status.Error(codes.FailedPrecondition,"库存分配数量异常")}
   if e=movement(ctx,tx,opID,c.UserID,domain.Line{SKUID:l.SKUID,Quantity:l.Quantity},kind);e!=nil{return e}
  }
  if e=tx.WithContext(ctx).Create(&domain.LineOperation{ID:opID,ReservationID:c.ID,Kind:kind,LinesJSON:encoded}).Error;e!=nil{return e}
  left:=int64(0);for _,n:=range remaining{left+=n};if left==0{reservation.State=constants.InventoryReservationStateCancelled;if dispatched>0{reservation.State=constants.InventoryReservationStateDispatched};return tx.WithContext(ctx).Model(&reservation).Update("state",reservation.State).Error};return nil
 })
 return platform.Map(reservation),e
}
