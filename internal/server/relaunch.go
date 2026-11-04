package server

import "os"

// startedFrom is this program's own file, read before anything can replace
// it: after an update swaps the file, Linux reports the running one as
// "(deleted)", while this path names the new program.
var startedFrom, _ = os.Executable()
