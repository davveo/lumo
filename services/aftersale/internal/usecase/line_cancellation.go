package usecase
import (
 "context"
 "encoding/json"
 "github.com/davveo/lumo/internal/constants"
 "github.com/davveo/lumo/internal/fulfillmentplan"
 "github.com/davveo/lumo/internal/platform"
 "github.com/davveo/lumo/services/aftersale/internal/domain"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
)
// stageLimits counts the original quantity available to each physical path.
// Cancelled lines remain in the refund-only pool: the claim ledger, rather than
// subtracting them twice, accounts for already requested cash refunds.
func stageLimits(o platform.Result,purchased map[string]int64,typ string)map[string]int64{
 shipped:=map[string]int64{}
 state:=platform.Text(o,"status")
 if state==constants.OrderStatusShipped||state==constants.OrderStatusCompleted{for k,n:=range purchased{shipped[k]=n}}
 if raw,ok:=o["fulfillment_progress"];ok&&raw!=nil{b,_:=json.Marshal(raw);var lines []fulfillmentplan.Progress;if json.Unmarshal(b,&lines)==nil&&len(lines)>0{shipped=map[string]int64{};for _,l:=range lines{shipped[l.SKUID]=l.Shipped+l.Dispatching}}}
 out:=map[string]int64{};for k,n:=range purchased{out[k]=shipped[k];if typ==constants.AftersaleTypeRefund{out[k]=n-shipped[k]}};return out
}
func holdRequest(r *domain.Record)platform.Request{var ls []domain.Line;_ = json.Unmarshal([]byte(r.LinesJSON),&ls);return platform.Request{ID:r.OrderID,UserID:r.UserID,Role:constants.RoleSystem,Data:map[string]any{"aftersale_id":r.ID,"lines":ls}}}
func (s *Service) approveRefund(ctx context.Context,r *domain.Record)(platform.Result,error){
 if r.Status==constants.AftersaleStatusApproved{return platform.Map(r),nil}
 r,e:=s.Repo.Transition(ctx,r.ID,[]string{constants.AftersaleStatusRequested},constants.AftersaleStatusApproving,nil,nil);if e!=nil{return nil,e}
 if _,e=platform.NewClients(s.RPC).Fulfillment.HoldLines(ctx,holdRequest(r));e!=nil{
  // A definitive warehouse rejection has no remote effect. Transient failures
  // remain recoverable because the warehouse may already have committed its hold.
  if status.Code(e)==codes.FailedPrecondition||status.Code(e)==codes.InvalidArgument{_,_ = s.Repo.Transition(ctx,r.ID,[]string{constants.AftersaleStatusApproving},constants.AftersaleStatusRequested,nil,nil)}
  return nil,e
 }
 r,e=s.Repo.Transition(ctx,r.ID,[]string{constants.AftersaleStatusApproving},constants.AftersaleStatusApproved,nil,nil);return platform.Map(r),e
}
func (s *Service) releaseRefund(ctx context.Context,r *domain.Record,reject bool)(platform.Result,error){
 next,pending:=constants.AftersaleStatusCancelled,constants.AftersaleStatusCancelling;if reject{next,pending=constants.AftersaleStatusRejected,constants.AftersaleStatusRejecting}
 if r.Status==next{return platform.Map(r),nil}
 if r.Status==constants.AftersaleStatusRequested{v,e:=s.Repo.Transition(ctx,r.ID,[]string{constants.AftersaleStatusRequested},next,nil,nil);return platform.Map(v),e}
 r,e:=s.Repo.Transition(ctx,r.ID,[]string{constants.AftersaleStatusApproved},pending,nil,nil);if e!=nil{return nil,e}
 if _,e=platform.NewClients(s.RPC).Fulfillment.ReleaseHold(ctx,holdRequest(r));e!=nil&&status.Code(e)!=codes.NotFound{return nil,e}
 // NotFound is safe for applications approved before line holds were introduced.
 r,e=s.Repo.Transition(ctx,r.ID,[]string{pending},next,nil,nil);return platform.Map(r),e
}
