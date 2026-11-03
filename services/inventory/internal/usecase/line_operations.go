package usecase
import (
 "context"
 "github.com/davveo/lumo/internal/constants"
 "github.com/davveo/lumo/internal/platform"
 "google.golang.org/grpc/codes"
 "google.golang.org/grpc/status"
)
func (s *Service) CancelLines(ctx context.Context,c platform.Request)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"inventory.usecase.CancelLines");defer sp.End()
 if c.Role!=constants.RoleSystem{return nil,status.Error(codes.PermissionDenied,"仅仓库内部命令可按行释放库存")};return s.Repo.ApplyLines(ctx,c,constants.InventoryMovementKindCancelled)
}
func (s *Service) DispatchLines(ctx context.Context,c platform.Request)(platform.Result,error){
 ctx,sp:=platform.Start(ctx,"inventory.usecase.DispatchLines");defer sp.End()
 if c.Role!=constants.RoleSystem{return nil,status.Error(codes.PermissionDenied,"仅仓库内部命令可按行扣减库存")};return s.Repo.ApplyLines(ctx,c,constants.InventoryMovementKindDispatched)
}
