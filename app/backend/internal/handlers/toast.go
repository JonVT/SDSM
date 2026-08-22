package handlers

import "github.com/gin-gonic/gin"

// SetToast sets standard toast headers used by the UI.
func SetToast(c *gin.Context, typ, title, msg string) {
	if c == nil {
		return
	}
	if typ != "" {
		c.Header("X-Toast-Type", typ)
	}
	if title != "" {
		c.Header("X-Toast-Title", title)
	}
	if msg != "" {
		c.Header("X-Toast-Message", msg)
	}
}

func ToastSuccess(c *gin.Context, title, msg string) { SetToast(c, "success", title, msg) }
func ToastInfo(c *gin.Context, title, msg string)    { SetToast(c, "info", title, msg) }
func ToastWarn(c *gin.Context, title, msg string)    { SetToast(c, "warning", title, msg) }
func ToastError(c *gin.Context, title, msg string)   { SetToast(c, "error", title, msg) }
