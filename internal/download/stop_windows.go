package download

import "os"

// terminate can't ask nicely on Windows; the caller uses aria2c's RPC instead.
func terminate(*os.Process) bool { return false }
