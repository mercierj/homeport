package aws

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type StepFunctionsAdapter struct {
	mu         sync.Mutex
	machines   map[string]stepFunctionsMachine
	executions map[string]stepFunctionsExecution
	quota      int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type StepFunctionsOption func(*StepFunctionsAdapter)

type stepFunctionsMachine struct {
	Arn        string
	Name       string
	Definition string
	RoleArn    string
	CreatedAt  time.Time
	Tags       map[string]string
}
type stepFunctionsExecution struct {
	Arn             string
	StateMachineArn string
	Name            string
	Input           string
	Status          string
	Error           string
	Cause           string
	StartedAt       time.Time
	StoppedAt       time.Time
}

func NewStepFunctionsAdapter(options ...StepFunctionsOption) *StepFunctionsAdapter {
	adapter := &StepFunctionsAdapter{machines: map[string]stepFunctionsMachine{}, executions: map[string]stepFunctionsExecution{}, authorizer: authz.AllowAll}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithStepFunctionsStateMachineQuota(maxMachines int) StepFunctionsOption {
	return func(adapter *StepFunctionsAdapter) { adapter.quota = maxMachines }
}
func WithStepFunctionsAuthorizer(authorizer authz.Authorizer) StepFunctionsOption {
	return func(adapter *StepFunctionsAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}
func WithStepFunctionsAuditSink(sink func(authz.Decision)) StepFunctionsOption {
	return func(adapter *StepFunctionsAdapter) { adapter.auditSink = sink }
}

func (StepFunctionsAdapter) Provider() string { return "aws" }
func (StepFunctionsAdapter) Service() string  { return "stepfunctions" }
func (StepFunctionsAdapter) Routes() []string { return []string{"POST /compat/aws/stepfunctions"} }
func (StepFunctionsAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_SFN":    "http://homeport:8080/api/v1/compat/aws/stepfunctions",
		"HOMEPORT_COMPAT_BACKEND": "temporal",
	}
}
func (StepFunctionsAdapter) ConformanceChecks() []string {
	return []string{"create-state-machine", "describe-state-machine", "list-state-machines", "update-state-machine", "delete-state-machine", "tag-resource", "list-tags-for-resource", "untag-resource", "start-execution", "describe-execution", "stop-execution", "list-executions", "get-execution-history"}
}

func (a *StepFunctionsAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeStepFunctionsError(w, "InvalidDefinition", err.Error())
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case "CreateStateMachine":
		name, definition := stringValue(body["name"]), stringValue(body["definition"])
		if !stepFunctionsNameValid(name) || !stepFunctionsDefinitionValid(definition) || stringValue(body["roleArn"]) == "" {
			writeStepFunctionsError(w, "InvalidDefinition", "name and definition are required")
			return
		}
		if _, exists := a.machines[name]; exists {
			writeStepFunctionsError(w, "StateMachineAlreadyExists", "state machine already exists")
			return
		}
		if a.quota > 0 && len(a.machines) >= a.quota {
			writeStepFunctionsError(w, "StateMachineLimitExceeded", "state machine quota exceeded")
			return
		}
		tags, ok := ecrTags(body["tags"])
		if !ok {
			writeStepFunctionsError(w, "ValidationException", "tags are invalid")
			return
		}
		machine := stepFunctionsMachine{Arn: stepFunctionsARN(name), Name: name, Definition: definition, RoleArn: stringValue(body["roleArn"]), CreatedAt: time.Now().UTC(), Tags: tags}
		a.machines[name] = machine
		writeJSON(w, http.StatusOK, map[string]any{"stateMachineArn": machine.Arn, "creationDate": machine.CreatedAt.Unix()})
	case "DescribeStateMachine":
		machine, ok := a.machine(body)
		if !ok {
			writeStepFunctionsError(w, "StateMachineDoesNotExist", "state machine does not exist")
			return
		}
		writeJSON(w, http.StatusOK, stepFunctionsShape(machine))
	case "ListStateMachines":
		names := make([]string, 0, len(a.machines))
		for name := range a.machines {
			names = append(names, name)
		}
		sort.Strings(names)
		start := 0
		if token := stringValue(body["nextToken"]); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil || parsed < 0 || parsed >= len(names) {
				writeStepFunctionsError(w, "InvalidToken", "nextToken is invalid")
				return
			}
			start = parsed
		}
		limit := 100
		if value, ok := body["maxResults"].(float64); ok && value > 0 && value <= 1000 {
			limit = int(value)
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		machines := make([]map[string]any, 0, end-start)
		for _, name := range names[start:end] {
			machine := a.machines[name]
			machines = append(machines, map[string]any{"stateMachineArn": machine.Arn, "name": machine.Name, "creationDate": machine.CreatedAt.Unix(), "type": "STANDARD"})
		}
		response := map[string]any{"stateMachines": machines}
		if end < len(names) {
			response["nextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "UpdateStateMachine":
		machine, ok := a.machine(body)
		if !ok {
			writeStepFunctionsError(w, "StateMachineDoesNotExist", "state machine does not exist")
			return
		}
		if definition := stringValue(body["definition"]); definition != "" {
			if !stepFunctionsDefinitionValid(definition) {
				writeStepFunctionsError(w, "InvalidDefinition", "definition is invalid")
				return
			}
			machine.Definition = definition
		}
		if roleArn := stringValue(body["roleArn"]); roleArn != "" {
			machine.RoleArn = roleArn
		}
		a.machines[machine.Name] = machine
		writeJSON(w, http.StatusOK, map[string]any{"updateDate": time.Now().UTC().Unix()})
	case "DeleteStateMachine":
		machine, ok := a.machine(body)
		if !ok {
			writeStepFunctionsError(w, "StateMachineDoesNotExist", "state machine does not exist")
			return
		}
		delete(a.machines, machine.Name)
		writeJSON(w, http.StatusOK, map[string]any{})
	case "ListTagsForResource":
		machine, ok := a.machine(body)
		if !ok {
			writeStepFunctionsError(w, "StateMachineDoesNotExist", "state machine does not exist")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tags": ecrTagShape(machine.Tags)})
	case "TagResource":
		machine, ok := a.machine(body)
		if !ok {
			writeStepFunctionsError(w, "StateMachineDoesNotExist", "state machine does not exist")
			return
		}
		tags, ok := ecrTags(body["tags"])
		if !ok {
			writeStepFunctionsError(w, "ValidationException", "tags are invalid")
			return
		}
		mergeStringMap(machine.Tags, tags)
		a.machines[machine.Name] = machine
		writeJSON(w, http.StatusOK, map[string]any{})
	case "UntagResource":
		machine, ok := a.machine(body)
		if !ok {
			writeStepFunctionsError(w, "StateMachineDoesNotExist", "state machine does not exist")
			return
		}
		for _, key := range ecrTagKeys(body["tagKeys"]) {
			delete(machine.Tags, key)
		}
		a.machines[machine.Name] = machine
		writeJSON(w, http.StatusOK, map[string]any{})
	case "StartExecution":
		machine, ok := a.machine(body)
		name, input := stringValue(body["name"]), stringValue(body["input"])
		if !ok || !stepFunctionsNameValid(name) || (input != "" && !json.Valid([]byte(input))) {
			writeStepFunctionsError(w, "ValidationException", "stateMachineArn, name, and JSON input are required")
			return
		}
		arn := stepFunctionsExecutionARN(machine.Name, name)
		if existing, exists := a.executions[arn]; exists {
			if existing.Input == input {
				writeJSON(w, http.StatusOK, map[string]any{"executionArn": existing.Arn, "startDate": existing.StartedAt.Unix()})
				return
			}
			writeStepFunctionsError(w, "ExecutionAlreadyExists", "execution already exists with different input")
			return
		}
		execution := stepFunctionsExecution{Arn: arn, StateMachineArn: machine.Arn, Name: name, Input: input, Status: "RUNNING", StartedAt: time.Now().UTC()}
		a.executions[arn] = execution
		writeJSON(w, http.StatusOK, map[string]any{"executionArn": execution.Arn, "startDate": execution.StartedAt.Unix()})
	case "DescribeExecution":
		execution, ok := a.executions[stringValue(body["executionArn"])]
		if !ok {
			writeStepFunctionsError(w, "ExecutionDoesNotExist", "execution does not exist")
			return
		}
		writeJSON(w, http.StatusOK, stepFunctionsExecutionShape(execution))
	case "StopExecution":
		execution, ok := a.executions[stringValue(body["executionArn"])]
		if !ok {
			writeStepFunctionsError(w, "ExecutionDoesNotExist", "execution does not exist")
			return
		}
		if execution.Status == "RUNNING" {
			execution.Status = "ABORTED"
			execution.Error = stringValue(body["error"])
			execution.Cause = stringValue(body["cause"])
			execution.StoppedAt = time.Now().UTC()
			a.executions[execution.Arn] = execution
		}
		writeJSON(w, http.StatusOK, map[string]any{"stopDate": execution.StoppedAt.Unix()})
	case "ListExecutions":
		stateMachineARN := stringValue(body["stateMachineArn"])
		if _, ok := a.machine(map[string]any{"stateMachineArn": stateMachineARN}); !ok {
			writeStepFunctionsError(w, "StateMachineDoesNotExist", "state machine does not exist")
			return
		}
		executions := make([]stepFunctionsExecution, 0)
		for _, execution := range a.executions {
			if execution.StateMachineArn == stateMachineARN {
				executions = append(executions, execution)
			}
		}
		sort.Slice(executions, func(i, j int) bool { return executions[i].Name < executions[j].Name })
		start := 0
		if token := stringValue(body["nextToken"]); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil || parsed < 0 || parsed >= len(executions) {
				writeStepFunctionsError(w, "InvalidToken", "nextToken is invalid")
				return
			}
			start = parsed
		}
		limit := 100
		if value, ok := body["maxResults"].(float64); ok && value > 0 && value <= 1000 {
			limit = int(value)
		}
		end := start + limit
		if end > len(executions) {
			end = len(executions)
		}
		items := make([]map[string]any, 0, end-start)
		for _, execution := range executions[start:end] {
			item := map[string]any{"executionArn": execution.Arn, "stateMachineArn": execution.StateMachineArn, "name": execution.Name, "status": execution.Status, "startDate": execution.StartedAt.Unix()}
			if !execution.StoppedAt.IsZero() {
				item["stopDate"] = execution.StoppedAt.Unix()
			}
			items = append(items, item)
		}
		response := map[string]any{"executions": items}
		if end < len(executions) {
			response["nextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "GetExecutionHistory":
		execution, ok := a.executions[stringValue(body["executionArn"])]
		if !ok {
			writeStepFunctionsError(w, "ExecutionDoesNotExist", "execution does not exist")
			return
		}
		events := []map[string]any{{"id": 1, "timestamp": execution.StartedAt.Unix(), "type": "ExecutionStarted"}}
		if !execution.StoppedAt.IsZero() {
			events = append(events, map[string]any{"id": 2, "previousEventId": 1, "timestamp": execution.StoppedAt.Unix(), "type": "ExecutionAborted"})
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	default:
		writeStepFunctionsError(w, "InvalidAction", "Step Functions action is not implemented")
	}
}

func (a *StepFunctionsAdapter) machine(body map[string]any) (stepFunctionsMachine, bool) {
	arn := stringValue(body["stateMachineArn"])
	if arn == "" {
		arn = stringValue(body["resourceArn"])
	}
	for _, machine := range a.machines {
		if machine.Arn == arn {
			return machine, true
		}
	}
	return stepFunctionsMachine{}, false
}
func (a *StepFunctionsAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	name := stringValue(body["name"])
	resource := stringValue(body["stateMachineArn"])
	if resource == "" {
		resource = stringValue(body["resourceArn"])
	}
	if resource == "" {
		resource = stepFunctionsARN(name)
	}
	decision, err := a.authorizer.Authorize(r.Context(), authz.Request{Principal: awsPrincipal(r), PrincipalAttributes: awsPrincipalAttributes(r), Action: "states:" + action, Resource: resource, Context: map[string]string{"provider": "aws", "service": "stepfunctions", "method": r.Method, "source_ip": sourceIP(r), "current_time": time.Now().UTC().Format(time.RFC3339), "user_agent": r.UserAgent()}, Claims: awsClaims(r)})
	if err != nil {
		writeStepFunctionsError(w, "InternalError", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"__type": "AccessDeniedException", "message": decision.Reason})
		return false
	}
	return true
}

func stepFunctionsARN(name string) string {
	return "arn:aws:states:us-east-1:000000000000:stateMachine:" + name
}
func stepFunctionsExecutionARN(machineName, executionName string) string {
	return "arn:aws:states:us-east-1:000000000000:execution:" + machineName + ":" + executionName
}
func stepFunctionsNameValid(name string) bool {
	if len(name) == 0 || len(name) > 80 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
func stepFunctionsDefinitionValid(definition string) bool {
	var value map[string]any
	return json.Unmarshal([]byte(definition), &value) == nil && value["StartAt"] != nil && value["States"] != nil
}
func stepFunctionsShape(machine stepFunctionsMachine) map[string]any {
	return map[string]any{"stateMachineArn": machine.Arn, "name": machine.Name, "definition": machine.Definition, "roleArn": machine.RoleArn, "creationDate": machine.CreatedAt.Unix(), "type": "STANDARD"}
}
func stepFunctionsExecutionShape(execution stepFunctionsExecution) map[string]any {
	shape := map[string]any{"executionArn": execution.Arn, "stateMachineArn": execution.StateMachineArn, "name": execution.Name, "input": execution.Input, "status": execution.Status, "startDate": execution.StartedAt.Unix()}
	if execution.Error != "" {
		shape["error"] = execution.Error
	}
	if execution.Cause != "" {
		shape["cause"] = execution.Cause
	}
	if !execution.StoppedAt.IsZero() {
		shape["stopDate"] = execution.StoppedAt.Unix()
	}
	return shape
}
func writeStepFunctionsError(w http.ResponseWriter, code, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"__type": code, "message": message})
}
