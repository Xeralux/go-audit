package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/spf13/viper"
	"log"
	"log/syslog"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	. "github.com/Xeralux/go-audit/client"
	"github.com/Xeralux/go-audit/logger"
	. "github.com/Xeralux/go-audit/marshaller"
	. "github.com/Xeralux/go-audit/writer"
)

var l = log.New(os.Stdout, "", 0)
var el = log.New(os.Stderr, "", 0)

type executor func(string, ...string) error

func lExec(s string, a ...string) error {
	return exec.Command(s, a...).Run()
}

func loadConfig(configFile string) (*viper.Viper, error) {
	config := viper.New()
	config.SetConfigFile(configFile)

	config.SetDefault("message_tracking.enabled", true)
	config.SetDefault("message_tracking.log_out_of_order", false)
	config.SetDefault("message_tracking.max_out_of_order", 500)
	config.SetDefault("output.syslog.enabled", false)
	config.SetDefault("output.syslog.priority", int(syslog.LOG_LOCAL0|syslog.LOG_WARNING))
	config.SetDefault("output.syslog.tag", "go-audit")
	config.SetDefault("output.syslog.attempts", "3")
	config.SetDefault("log.flags", 0)

	if err := config.ReadInConfig(); err != nil {
		return nil, err
	}

	l.SetFlags(config.GetInt("log.flags"))
	el.SetFlags(config.GetInt("log.flags"))

	return config, nil
}

func setRules(config *viper.Viper, e executor) error {
	// Clear existing rules
	if err := e("auditctl", "-D"); err != nil {
		return errors.New(fmt.Sprintf("Failed to flush existing audit rules. Error: %s", err))
	}

	logger.Info("Flushed existing audit rules")

	// Add ours in
	if rules := config.GetStringSlice("rules"); len(rules) != 0 {
		for i, v := range rules {
			// Skip rules with no content
			if v == "" {
				continue
			}

			if err := e("auditctl", strings.Fields(v)...); err != nil {
				return errors.New(fmt.Sprintf("Failed to add rule #%d. Error: %s", i+1, err))
			}

			logger.Info("Added audit rule #%d", i+1)
		}
	} else {
		return errors.New("No audit rules found.")
	}

	return nil
}

func createOutput(config *viper.Viper) (*AuditWriter, error) {
	var writer *AuditWriter
	var err error
	i := 0

	if config.GetBool("output.syslog.enabled") == true {
		i++
		writer, err = createSyslogOutput(config)
		if err != nil {
			return nil, err
		}
	}

	if config.GetBool("output.file.enabled") == true {
		i++
		writer, err = createFileOutput(config)
		if err != nil {
			return nil, err
		}
	}

	if config.GetBool("output.stdout.enabled") == true {
		i++
		writer, err = createStdOutOutput(config)
		if err != nil {
			return nil, err
		}
	}

	if i > 1 {
		return nil, errors.New("Only one output can be enabled at a time")
	}

	if writer == nil {
		return nil, errors.New("No outputs were configured")
	}

	return writer, nil
}

func createSyslogOutput(config *viper.Viper) (*AuditWriter, error) {
	attempts := config.GetInt("output.syslog.attempts")
	if attempts < 1 {
		return nil, errors.New(
			fmt.Sprintf("Output attempts for syslog must be at least 1, %v provided", attempts),
		)
	}

	syslogWriter, err := syslog.Dial(
		config.GetString("output.syslog.network"),
		config.GetString("output.syslog.address"),
		syslog.Priority(config.GetInt("output.syslog.priority")),
		config.GetString("output.syslog.tag"),
	)

	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to open syslog writer. Error: %v", err))
	}

	return NewAuditWriter(syslogWriter, attempts), nil
}

func createFileOutput(config *viper.Viper) (*AuditWriter, error) {
	attempts := config.GetInt("output.file.attempts")
	if attempts < 1 {
		return nil, errors.New(
			fmt.Sprintf("Output attempts for file must be at least 1, %v provided", attempts),
		)
	}

	mode := os.FileMode(config.GetInt("output.file.mode"))
	if mode < 1 {
		return nil, errors.New("Output file mode should be greater than 0000")
	}

	f, err := os.OpenFile(
		config.GetString("output.file.path"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, mode,
	)

	if err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to open output file. Error: %s", err))
	}

	if err := f.Chmod(mode); err != nil {
		return nil, errors.New(fmt.Sprintf("Failed to set file permissions. Error: %s", err))
	}

	uname := config.GetString("output.file.user")
	u, err := user.Lookup(uname)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Could not find uid for user %s. Error: %s", uname, err))
	}

	gname := config.GetString("output.file.group")
	g, err := user.LookupGroup(gname)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Could not find gid for group %s. Error: %s", gname, err))
	}

	uid, err := strconv.ParseInt(u.Uid, 10, 32)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Found uid could not be parsed. Error: %s", err))
	}

	gid, err := strconv.ParseInt(g.Gid, 10, 32)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Found gid could not be parsed. Error: %s", err))
	}

	if err = f.Chown(int(uid), int(gid)); err != nil {
		return nil, errors.New(fmt.Sprintf("Could not chown output file. Error: %s", err))
	}

	return NewAuditWriter(f, attempts), nil
}

func createStdOutOutput(config *viper.Viper) (*AuditWriter, error) {
	attempts := config.GetInt("output.stdout.attempts")
	if attempts < 1 {
		return nil, errors.New(
			fmt.Sprintf("Output attempts for stdout must be at least 1, %v provided", attempts),
		)
	}

	// l logger is no longer stdout
	l.SetOutput(os.Stderr)

	return NewAuditWriter(os.Stdout, attempts), nil
}

func createFilters(config *viper.Viper) []AuditFilter {
	var err error
	var ok bool

	fs := config.Get("filters")
	filters := []AuditFilter{}

	if fs == nil {
		return filters
	}

	ft, ok := fs.([]interface{})
	if !ok {
		return filters
	}

	for i, f := range ft {
		f2, ok := f.(map[interface{}]interface{})
		if !ok {
			logger.Crit("Could not parse filter %d, %v", i+1, f)
			panic("Could not parse filter")
		}

		af := AuditFilter{}
		for k, v := range f2 {
			switch k {
			case "message_type":
				if ev, ok := v.(string); ok {
					fv, err := strconv.ParseUint(ev, 10, 64)
					if err != nil {
						logger.Crit("`message_type` in filter %d could not be parsed %v (%v) ", i+1, v, err)
						panic(err)
					}
					af.MessageType = uint16(fv)

				} else if ev, ok := v.(int); ok {
					if !ok {
						logger.Crit("`message_type` in filter %d could not be parsed %v", i+1,  v)
						panic("`message_type` in filter could not be parsed")
					}
					af.MessageType = uint16(ev)

				} else {
					logger.Crit("`message_type` in filter %d could not be parsed %v", i+1,  v)
					panic("`message_type` in filter could not be parsed")
				}

			case "regex":
				re, ok := v.(string)
				if !ok {
					logger.Crit("`regex` in filter %d could not be parsed %v", i+1,  v)
					panic("`regex` in filter could not be parsed")
				}

				if af.Regex, err = regexp.Compile(re); err != nil {
					logger.Crit("`regex` in filter %d could not be parsed %v", i+1,  v)
					panic(err)
				}

			case "syscall":
				if af.Syscall, ok = v.(string); ok {
					// All is good
				} else if ev, ok := v.(int); ok {
					af.Syscall = strconv.Itoa(ev)
				} else {
					logger.Crit("`syscall` in filter %d could not be parsed %v", i+1,  v)
					panic("`syscall` in filter could not be parsed")
				}
			}
		}

		filters = append(filters, af)
		logger.Info("Ignoring  syscall `%v` containing message type `%v` matching string `%s`\n",
			af.Syscall, af.MessageType, af.Regex.String())
	}

	return filters
}

func main() {
	configFile := flag.String("config", "", "Config file location")

	flag.Parse()

	logger.AuditLoggerNew(l, el, nil)

	if *configFile == "" {
		logger.Err("A config file must be provided")
		flag.Usage()
		os.Exit(1)
	}

	config, err := loadConfig(*configFile)
	if err != nil {
		logger.Crit("%v", err)
		panic(err)
	}

	// output needs to be created before anything that write to stdout
	writer, err := createOutput(config)
	if err != nil {
		logger.Crit("%v", err)
		panic(err)
	}

	if err := setRules(config, lExec); err != nil {
		logger.Crit("%v", err)
		panic(err)
	}

	nlClient := NewNetlinkClient(config.GetInt("socket_buffer.receive"))
	marshaller := NewAuditMarshaller(
		writer,
		config.GetBool("message_tracking.enabled"),
		config.GetBool("message_tracking.log_out_of_order"),
		config.GetInt("message_tracking.max_out_of_order"),
		createFilters(config),
	)

	logger.Info("Started processing events")

	//Main loop. Get data from netlink and send it to the json lib for processing
	for {
		msg, err := nlClient.Receive()
		if err != nil {
			logger.Err("Error during message receive: %+v", err)
			continue
		}

		if msg == nil {
			continue
		}

		marshaller.Consume(msg)
	}
}
