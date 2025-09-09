package common

import (
    "log"
    "io/ioutil"
)

func LogStdOut(logApp bool) {
    if logApp {
        log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile | log.LUTC)
    } else {
        log.SetOutput(ioutil.Discard)
    }
}
