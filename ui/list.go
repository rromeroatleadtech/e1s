package ui

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	ecsTypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

const (
	logFmt = "[aqua::]%s[-:-:-]:%s\n"
)

// Show new page from LIST(like logs, events) content in table area and handle done event to go back
func (v *View) showListPages(entity Entity, which string) {
	contentString := v.getListString(entity, which)
	v.handleContentPageSwitch(entity, which, contentString)
	v.handleInfoPageSwitch(entity, LogPage)
}

// Based on current entity return list string as content
func (v *View) getListString(entity Entity, which string) string {
	contentString := ""
	tz := os.Getenv("TZ")
	currentTz, _ := time.LoadLocation(tz)

	switch which {
	case "events":
		if entity.service == nil {
			contentString += "[red::]No valid contents[-:-:-]"
		}
		for _, e := range entity.events {
			createdAt := e.CreatedAt.In(currentTz)
			contentString += fmt.Sprintf(logFmt, createdAt.Format(time.RFC3339), *e.Message)
		}
	case "logs":
		var logs []types.OutputLogEvent
		var err error
		var tdArn *string
		if entity.service != nil {
			tdArn = entity.service.TaskDefinition
		} else if entity.task != nil {
			tdArn = entity.task.TaskDefinitionArn
		}

		logs, err = v.app.Store.GetLogs(tdArn)

		if err != nil {
			contentString += "[red::]No valid contents[-:-:-]"
		}

		for _, log := range logs {
			m := log.Message
			contentString += fmt.Sprintf(logFmt, time.Unix(0, *log.Timestamp*int64(time.Millisecond)).Format(time.RFC3339), *m)
		}
	}

	return contentString
}

// Switch to selected service events JSON page
func (v *View) switchToServiceEventsList() {
	selected, err := v.getCurrentSelection()
	if err != nil {
		return
	}
	if v.kind != ServicePage {
		return
	}
	v.showListPages(selected, "events")
}

// Switch to selected service events JSON page
func (v *View) switchToLogsList() {
	if v.kind == ClusterPage || v.kind == ContainerPage {
		return
	}
	selected, err := v.getCurrentSelection()
	if err != nil {
		return
	}
	v.secondaryKind = LogPage
	v.showListPages(selected, "logs")
}

func (v *View) realtimeAwsLog(entity Entity) {
	var tdArn *string
	var logGroup string
	var canRealtime bool
	if entity.service != nil {
		tdArn = entity.service.TaskDefinition
	} else if entity.task != nil {
		tdArn = entity.task.TaskDefinitionArn
	}
	if tdArn == nil {
		return
	}
	td, err := v.app.Store.DescribeTaskDefinition(tdArn)
	if err != nil {
		return
	}
	for _, c := range td.ContainerDefinitions {
		// if current container has no log driver
		if c.LogConfiguration.LogDriver != ecsTypes.LogDriverAwslogs {
			continue
		}

		groupName := c.LogConfiguration.Options["awslogs-group"]
		// if current container log configuration has no awslogs-group
		if groupName == "" {
			continue
		}

		// if logGroup is empty, assign it, can realtime logs
		if logGroup == "" {
			logGroup = groupName
			canRealtime = true
		} else {
			// if groupName is the same with previous
			if logGroup == groupName {
				continue
				// if groupName is the different, can not realtime logs
			} else {
				canRealtime = false
			}
		}
	}

	if canRealtime {
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

		bin, err := exec.LookPath(awsCli)
		if err != nil {
			logger.Printf("e1s - aws cli binary not found, error: %v\n", err)
			v.back()
		}
		arg := []string{
			"logs",
			"tail",
			"--follow",
			logGroup,
		}

		logger.Printf("%s %s\n", awsCli, strings.Join(arg, " "))

		v.app.Suspend(func() {
			cmd := exec.Command(bin, arg...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			_, err = cmd.Stdout.Write([]byte(fmt.Sprintf(realtimeLogFmt, *v.app.cluster.ClusterName, *v.app.service.ServiceName, logGroup)))
			err = cmd.Run()
			// return signal
			signal.Stop(interrupt)
			close(interrupt)
		})

	}

}
