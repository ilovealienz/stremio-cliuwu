package poster

import ()

// AsyncID tags in-flight loads. Bubble Tea delivers messages to whatever screen
// is on top, so a load that finishes after you've navigated away would
// otherwise land on the wrong screen.
type AsyncID int

var asyncSeq AsyncID

func NewAsyncID() AsyncID { asyncSeq++; return asyncSeq }
