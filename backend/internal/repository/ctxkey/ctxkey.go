// Package ctxkey owns the gin-context key used to share the per-request
// transaction between RLSTxMiddleware and repository WithCtx helpers.
// Keeping the constant here avoids duplicating the string literal across
// 18 repositories and the api package.
package ctxkey

// Tx is the gin context key under which RLSTxMiddleware stashes the per-
// request *sql.Tx. Repository WithCtx helpers read this key.
const Tx = "tx"

// CtxGetter is the minimal subset of *gin.Context needed to extract a
// per-request transaction. Using a structural interface lets repository
// code accept a gin.Context without importing gin.
type CtxGetter interface {
	Get(key string) (any, bool)
}
