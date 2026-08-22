package constants

import (
	"os"
	"strings"
)

// SDSMCommunityBugReportWebhookEnv is the environment variable used to resolve
// the bug-report webhook at runtime. Keeping this external avoids hardcoding a
// live secret in source and binaries.
const SDSMCommunityBugReportWebhookEnv = "SDSM_COMMUNITY_BUG_REPORT_WEBHOOK"

// SDSMCommunityBugReportWebhook returns the configured bug-report webhook URL.
// Empty string means bug-report delivery is not configured.
func SDSMCommunityBugReportWebhook() string {
	return strings.TrimSpace(os.Getenv(SDSMCommunityBugReportWebhookEnv))
}
