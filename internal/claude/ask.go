package claude

import "time"

// AskWait is how long a terminal's AskUserQuestion / ExitPlanMode stays
// answerable from the web: a week, so a question asked while the user is away
// can still be answered remotely when they come back. The host gives up first,
// then the hook, then Claude Code's hook timeout, so a long wait is recorded as
// expired rather than as answered in the terminal.
const AskWait = 7 * 24 * time.Hour

// AskHookTimeout is the PermissionRequest hook's timeout in settings (seconds).
const AskHookTimeout = int((AskWait + 2*time.Minute) / time.Second)
