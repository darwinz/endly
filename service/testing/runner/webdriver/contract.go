package webdriver

import (
	"fmt"
	"github.com/viant/endly/internal/util"
	"github.com/viant/endly/model/criteria/eval"
	"github.com/viant/endly/model/location"
	"github.com/viant/endly/service/testing/validator"
	"github.com/viant/toolbox"
	"github.com/viant/toolbox/data"
	"strings"
)

const defaultTarget = "/opt/local/webdriver"
const defaultExitWaitTimeMs = 1000

type PathKind int

const (
	PathKindUndefined = PathKind(iota)
	PathKindSimple
	PathKindComposite
)

// StartRequest represents a selenium server start request
type StartRequest struct {
	Target       *location.Resource
	Driver       string
	Server       string
	Sdk          string
	Capabilities []string
	Port         int
}

func (r *StartRequest) Init() error {
	if r.Port == 0 {
		r.Port = 4444
	}
	if r.Driver == "" {
		r.Driver = ChromeDriver
	}
	if r.Target == nil {
		r.Target = location.NewResource(defaultTarget)
	}
	return nil
}

func (r *StartRequest) Validate() error {
	return nil
}

// NewStartRequestFromURL creates a new start request from URL
func NewStartRequestFromURL(URL string) (*StartRequest, error) {
	var result = &StartRequest{}
	var resource = location.NewResource(URL)
	err := resource.Decode(result)
	return result, err
}

// StartResponse represents a selenium server stop request
type StartResponse struct {
	Pid        int
	ServerPath string
	DriverPath string
	SessionID  string
}

// StopRequest represents server stop request
type StopRequest struct {
	Target *location.Resource
	Port   int
}

func (r *StopRequest) Init() error {
	if r.Port == 0 {
		r.Port = 4444
	}

	if r.Target == nil {
		r.Target = location.NewResource(defaultTarget)
	}
	return nil

}

// NewStopRequestFromURL creates a new start request from URL
func NewStopRequestFromURL(URL string) (*StopRequest, error) {
	var result = &StopRequest{}
	var resource = location.NewResource(URL)
	err := resource.Decode(result)
	return result, err
}

// StopResponse represents a selenium stop request
type StopResponse struct {
}

// OpenSessionResponse represents open session response.
type OpenSessionResponse struct {
	SessionID string
}

// CloseSessionRequest represents close session request.
type CloseSessionRequest struct {
	SessionID string
}

// NewCloseSessionRequestFromURL creates a new close session request from URL
func NewCloseSessionRequestFromURL(URL string) (*CloseSessionRequest, error) {
	var result = &CloseSessionRequest{}
	var resource = location.NewResource(URL)
	err := resource.Decode(result)
	return result, err
}

// CloseSessionResponse represents close session response.
type CloseSessionResponse struct {
	SessionID string
}

// WebDriverCallRequest represents selenium call driver request
type WebDriverCallRequest struct {
	SessionID string
	Key       string
	PathKind  PathKind
	Call      *MethodCall
}

// ServiceCallResponse represents selenium call response
type ServiceCallResponse struct {
	Result []interface{}
	Data   data.Map
}

// WebElementSelector represents a web element selector
type WebElementSelector struct {
	By    string //selector type
	Value string //selector value
	Key   string //optional result key
}

// WebElementCallRequest represents a web element call reqesut
type WebElementCallRequest struct {
	SessionID string
	Selector  *WebElementSelector
	Call      *MethodCall
	PathKind  PathKind
}

// WebElementCallResponse represents seleniun web element response
type WebElementCallResponse struct {
	Result      []interface{}
	LookupError string
	Data        map[string]interface{}
}

// RunRequest represents group of selenium web elements calls
type RunRequest struct {
	SessionID      string
	Browser        string
	RemoteSelenium string //remote selenium resource
	Actions        []*Action
	ActionDelaysMs int           `description:"slows down action with specified delay"`
	Commands       []interface{} `description:"list of selenium command: {web element selector}.WebElementMethod(params),  or WebDriverMethod(params), or wait map "`
	Expect         interface{}   `description:"If specified it will validated response as actual"`
}

func (r *RunRequest) asWaitAction(parser *parser, candidate interface{}) (*Action, error) {
	if aMap, err := util.NormalizeMap(candidate, true); err == nil {
		command, ok := aMap["command"]
		if !ok {
			return nil, fmt.Errorf("command was missing: %v", candidate)
		}
		action, err := parser.Parse(toolbox.AsString(command))
		if err != nil {
			return nil, err
		}
		if action.PathKind == PathKindUndefined {
			action.PathKind = PathKindSimple
		}
		err = toolbox.DefaultConverter.AssignConverted(&action.Calls[0].Wait, aMap)
		return action, err
	}
	return nil, fmt.Errorf("sunupported command: %T", candidate)
}

func (r *RunRequest) Init() error {
	if r.SessionID == "" {
		r.SessionID = "localhost:4444"
	}
	if len(r.Actions) > 0 {
		for _, action := range r.Actions {
			if action.Selector != nil {
				_ = action.Selector.Init()
				if action.Selector.Key == "" {
					action.Selector.Key = action.Key
				}
				if action.Selector.Key == "" {
					action.Selector.Key = action.Selector.Value
				}
			}
		}
		return nil
	}
	if len(r.Commands) == 0 {
		return nil
	}

	expectMap := r.expectMap()
	r.Actions = make([]*Action, 0)
	var previousAction *Action
	parser := &parser{}
	for _, candidate := range r.Commands {
		command, ok := candidate.(string)
		if !ok {
			action, err := r.asWaitAction(parser, candidate)
			if err != nil {
				return err
			}
			r.setWaitExitIfNeeded(action.Calls[0], expectMap, action)
			r.Actions = append(r.Actions, action)
			continue
		}
		action, err := parser.Parse(command)
		if err != nil {
			return fmt.Errorf("invalid command: %v, %v", command, err)
		}
		if previousAction != nil {
			if previousAction.Selector != nil && action.Selector != nil && previousAction.Selector.Value == action.Selector.Value {
				if action.Key == "" && isReadMethod(action.Calls[0].Method) && isReadMethod(previousAction.Calls[0].Method) {
					previousAction.Calls = append(previousAction.Calls, action.Calls[0])
					previousAction.PathKind = PathKindComposite
					continue
				}
			}
		}
		r.Actions = append(r.Actions, action)
		previousAction = action
		call := action.Calls[0]
		r.setWaitExitIfNeeded(call, expectMap, action)

	}
	return nil
}

func (r *RunRequest) setWaitExitIfNeeded(call *MethodCall, expectMap map[string]interface{}, action *Action) {
	if call.Exit == "" {
		if expectValue, ok := expectMap[action.Key]; ok && expectValue != nil {
			switch actual := expectValue.(type) {
			case string:
				call.Exit = "$" + action.Key + " contains " + actual
			case int, int64, int32, int16, int8, uint, uint64, uint32, uint16, uint8:
				call.Exit = "$" + action.Key + " = " + toolbox.AsString(expectValue)
			case float64:
				call.Exit = "$" + action.Key + " = " + toolbox.AsString(expectValue)
			}
			if call.Exit != "" {
				if call.WaitTimeMs == 0 {
					call.WaitTimeMs = defaultExitWaitTimeMs
				}
				call.IgnoreTimeout = true
			}
		}
	}
}

func isReadMethod(method string) bool {
	return strings.HasPrefix(method, "Get") || strings.HasPrefix(method, "Text")
}

// NewRunRequest creates a new run request
func NewRunRequest(sessionID, browser string, remote string, actions ...*Action) *RunRequest {
	return &RunRequest{
		SessionID:      sessionID,
		Browser:        browser,
		RemoteSelenium: remote,
		Actions:        actions,
	}
}

// NewRunRequestFromURL creates a new request from URL
func NewRunRequestFromURL(URL string) (*RunRequest, error) {
	resource := location.NewResource(URL)
	var result = &RunRequest{}
	return result, resource.Decode(result)
}

// RunResponse represents selenium call response
type RunResponse struct {
	SessionID    string
	Data         map[string]interface{}
	LookupErrors []string
	Assert       *validator.AssertResponse
}

// MethodCall represents selenium call.
type MethodCall struct {
	Wait
	Method     string
	Parameters []interface{}
}

type Wait struct {
	WaitTimeMs    int
	ThinkTimeMs   int
	IgnoreTimeout bool
	Exit          string
	criteria      eval.Compute
}

// Action represents various calls on web element
type Action struct {
	Key string //optional result key
	PathKind
	Selector *WebElementSelector
	Calls    []*MethodCall
}

// NewAction creates a new action
func NewAction(key, selector string, method string, params ...interface{}) *Action {
	var result = &Action{
		Key: key,
		Calls: []*MethodCall{
			{
				Method:     method,
				Parameters: params,
			},
		},
	}
	if selector != "" {
		var webSelector = WebSelector(selector)
		result.Selector = &WebElementSelector{}
		result.Selector.By, result.Selector.Value = webSelector.ByAndValue()
		result.Selector.Key = result.Key
	}
	return result
}

// Validate validates run request.
func (r *RunRequest) Validate() error {
	if r.SessionID == "" {
		if r.Browser == "" {
			return fmt.Errorf("both SessionID and Browser were empty")
		}
	}

	if len(r.Actions) == 0 {
		return fmt.Errorf("both actions/commands were empty")
	}

	for i, action := range r.Actions {
		if len(action.Calls) == 0 {
			return fmt.Errorf("actions[%d].Calls were empty", i)
		}
	}
	return nil
}

// OpenSessionRequest represents open session request
type OpenSessionRequest struct {
	Browser      string
	Capabilities []string
	Remote       string `description:"webdriver server endpoint"`
	SessionID    string `description:"if specified this SessionID will be used for a sessionID"`
}

// Init  initializes request
func (r *OpenSessionRequest) Init() error {
	if r.SessionID == "" {
		r.SessionID = "localhost:4444"
	}
	host, port := pair(r.SessionID)
	r.Remote = fmt.Sprintf("http://%v:%v/wd/hub", host, port)
	return nil
}

// NewOpenSessionRequest creates a new open session request
func NewOpenSessionRequest(browser string, remote string) *OpenSessionRequest {
	return &OpenSessionRequest{
		Browser: browser,
		Remote:  remote,
	}
}

func (r *RunRequest) expectMap() map[string]interface{} {
	var expectMap = map[string]interface{}{}
	if r.Expect != nil {
		if value, ok := r.Expect.(map[string]interface{}); ok {
			expectMap = value
		}
	}
	return expectMap
}
