package logger

import (
	"fmt"
	"log"
	"log/syslog"
	"path"
	"runtime"
)

var stdOut	*log.Logger
var stdErr	*log.Logger
var sysLog	*syslog.Writer

func AuditLoggerNew(so *log.Logger, se *log.Logger, sl *syslog.Writer) {
	stdOut = so
	stdErr = se
	sysLog = sl
}

func fmtLog (format string, a ...interface{}) string {
	_, file, line, _ := runtime.Caller (2)
	str := fmt.Sprintf (format, a...)
	str = fmt.Sprintf ("%v (%v): %v", path.Base (file), line, str)

	return str
}

func Emerg (format string, a ...interface{}) (err error) {
	if sysLog != nil {
		err = sysLog.Emerg (fmtLog (format, a...))
	} else if stdErr != nil {
		stdErr.Printf(format, a)
	}
	return err
}

func Alert (format string, a ...interface{}) (err error) {
	if sysLog != nil {
		err = sysLog.Alert (fmtLog (format, a...))
	} else if stdErr != nil {
		stdErr.Printf(format, a)
	}
	return err
}

func Crit (format string, a ...interface{}) (err error) {
	if sysLog != nil {
		err = sysLog.Crit (fmtLog (format, a...))
	} else if stdErr != nil {
		stdErr.Printf(format, a)
	}
	return err
}

func Err (format string, a ...interface{}) (err error) {
	if sysLog != nil {
		err = sysLog.Err (fmtLog (format, a...))
	} else if stdErr != nil {
		stdErr.Printf(format, a)
	}
	return err
}

func Warning (format string, a ...interface{}) (err error) {
	if sysLog != nil {
		err = sysLog.Warning (fmtLog (format, a...))
	} else if stdErr != nil {
		stdErr.Printf(format, a)
	}

	return err
}

func Notice (format string, a ...interface{}) (err error) {
	if sysLog != nil {
		err = sysLog.Notice (fmtLog (format, a...))
	} else if stdOut != nil {
		stdOut.Printf(format, a)
	}

	return err
}

func Info (format string, a ...interface{}) (err error) {
	if sysLog != nil {
		err = sysLog.Info (fmtLog (format, a...))
	} else if stdOut != nil {
		stdOut.Printf(format, a)
	}

	return err
}

func Debug (format string, a ...interface{}) (err error) {
	if sysLog != nil {
		err = sysLog.Debug (fmtLog (format, a...))
	} else if stdOut != nil {
		stdOut.Printf(format, a)
	}

	return err
}
