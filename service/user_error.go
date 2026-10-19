package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const (
	userErrorRoleContextKey          = "user_error_role"
	userErrorResponseModelContextKey = "user_error_response_model"
)

// ResolveUserRole resolves API-key owners as well as dashboard roles. Callers
// can use the cached role when they need the same administrator boundary for
// logs and task diagnostics.
func ResolveUserRole(c *gin.Context) int {
	if c == nil {
		return common.RoleCommonUser
	}
	if cached, ok := c.Get(userErrorRoleContextKey); ok {
		if role, ok := cached.(int); ok {
			return role
		}
	}
	role := common.RoleCommonUser
	if value, ok := c.Get("role"); ok {
		if statedRole, ok := value.(int); ok {
			role = statedRole
		}
	} else if c.GetInt("id") > 0 && model.DB != nil {
		if user, err := model.GetUserCache(c.GetInt("id")); err == nil && user != nil {
			role = user.Role
		}
	}
	c.Set(userErrorRoleContextKey, role)
	return role
}

// SetUserErrorResponseModel records the upstream response declaration for the
// final error projection. The value is copied because RelayInfo may be reused.
func SetUserErrorResponseModel(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil || info == nil || info.ResponseModel == nil {
		return
	}
	c.Set(userErrorResponseModelContextKey, *info.ResponseModel)
}

// NewUserErrorPresentation resolves API-key owners as well as dashboard roles.
// A nil result preserves administrator diagnostics; failed lookups stay private.
func NewUserErrorPresentation(c *gin.Context) *model.UserErrorPresentation {
	if c == nil {
		return model.NewUserErrorPresentation(0, "")
	}
	if ResolveUserRole(c) >= common.RoleAdminUser {
		return nil
	}
	var mapping map[string]string
	_ = common.UnmarshalJsonStr(c.GetString("model_mapping"), &mapping)
	upstreamModels := make([]string, 0, len(mapping)+3)
	for _, name := range mapping {
		upstreamModels = append(upstreamModels, name)
	}
	requested := c.GetString("original_model")
	if raw, ok := c.Get(userErrorResponseModelContextKey); ok {
		switch observation := raw.(type) {
		case relaycommon.ResponseModel:
			if requested == "" {
				requested = observation.RequestedModel
			}
			upstreamModels = append(upstreamModels, observation.UpstreamModel, observation.ReturnedModel)
		case *relaycommon.ResponseModel:
			if observation != nil {
				if requested == "" {
					requested = observation.RequestedModel
				}
				upstreamModels = append(upstreamModels, observation.UpstreamModel, observation.ReturnedModel)
			}
		}
	}
	return model.NewUserErrorPresentation(c.GetInt("channel_id"), requested, upstreamModels...)
}

func PresentUserErrorJSON(c *gin.Context, data []byte, status int) []byte {
	if status < 400 && !model.IsRelayErrorJSON(data) {
		return data
	}
	return NewUserErrorPresentation(c).PresentJSON(data, status)
}
