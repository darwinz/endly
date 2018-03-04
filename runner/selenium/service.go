package selenium

import (
	"errors"
	"fmt"
	"github.com/tebeka/selenium"
	"github.com/viant/endly"
	"github.com/viant/endly/deployment/deploy"
	"github.com/viant/endly/deployment/sdk"
	"github.com/viant/endly/system/process"
	"github.com/viant/toolbox"
	"github.com/viant/toolbox/data"
	"github.com/viant/toolbox/url"
	"strings"
)

const (
	//ServiceID represents a ServiceID
	ServiceID = "selenium"

	//Selenium represents name of selenium server
	Selenium = "selenium-server-standalone"
	//GeckoDriver represents name of gecko driver
	GeckoDriver = "geckodriver"

	runnerCaller = "runnerCaller"
)

type service struct {
	*endly.AbstractService
}

func (s *service) addResultIfPresent(callResult []interface{}, result data.Map, resultPath ...string) {
	var responseData string
	var has = false
	for _, element := range callResult {
		if element == nil || !toolbox.IsString(element) {
			continue
		}
		has = true
		responseData = toolbox.AsString(element)
		break
	}
	if !has {
		return
	}
	var key = strings.Join(resultPath, ".")
	result.SetValue(key, responseData)
}

func (s *service) run(context *endly.Context, request *RunRequest) (*RunResponse, error) {
	var response = &RunResponse{
		Data:         make(map[string]interface{}),
		LookupErrors: make([]string, 0),
	}
	var result = data.Map(response.Data)

	if request.SessionID == "" {
		openResponse, err := s.openSession(context, &OpenSessionRequest{
			RemoteSelenium: request.RemoteSelenium,
			Browser:        request.Browser,
		})

		if err != nil {
			return nil, err
		}
		request.SessionID = openResponse.ID
	}
	response.SessionID = request.SessionID
	if len(request.Actions) == 0 {
		return response, nil
	}
	for _, action := range request.Actions {
		for _, call := range action.Calls {
			if action.Selector == nil {
				callResponse, err := s.callWebDriver(context, &WebDriverCallRequest{
					SessionID: request.SessionID,
					Call:      call,
				})
				if err != nil {
					return nil, err
				}
				s.addResultIfPresent(callResponse.Result, result, call.Method)
				continue

			}

			callResponse, err := s.callWebElement(context, &WebElementCallRequest{
				SessionID: request.SessionID,
				Selector:  action.Selector,
				Call:      call,
			})
			if err != nil {
				return nil, err
			}
			if callResponse.LookupError != "" {
				response.LookupErrors = append(response.LookupErrors, callResponse.LookupError)
			}
			var elementKey = action.Selector.Key
			if elementKey == "" {
				elementKey = action.Selector.Value
			}
			var elementPath = []string{elementKey, call.Method}
			if len(call.Parameters) == 1 && toolbox.IsString(call.Parameters[0]) {
				elementPath[1] = strings.Replace(elementPath[1], "Get", "", 1)
				elementPath[1] = strings.Replace(elementPath[1], "Property", "", 1)
				elementPath = append(elementPath)
			}
			s.addResultIfPresent(callResponse.Result, result, elementPath...)
		}
	}
	return response, nil
}

func (s *service) callMethod(owner interface{}, methodName string, parameters []interface{}) (*ServiceCallResponse, error) {
	method, err := toolbox.GetFunction(owner, methodName)
	if err != nil {
		return nil, err
	}
	parameters, err = toolbox.AsCompatibleFunctionParameters(method, parameters)
	if err != nil {
		return nil, err
	}
	var response = &ServiceCallResponse{}
	response.Result = toolbox.CallFunction(method, parameters...)
	return response, nil
}

func (s *service) callWebDriver(context *endly.Context, request *WebDriverCallRequest) (*ServiceCallResponse, error) {
	seleniumSession, err := s.session(context, request.SessionID)
	if err != nil {
		return nil, err
	}
	return s.call(context, seleniumSession.driver, request.Call)
}

func (s *service) call(context *endly.Context, caller interface{}, call *MethodCall) (callResponse *ServiceCallResponse, err error) {
	callResponse = &ServiceCallResponse{
		Extracted: make(map[string]string),
	}
	repeatable := call.Wait.Get()
	var handler = func() (interface{}, error) {
		callResponse, err = s.callMethod(caller, call.Method, call.Parameters)
		if err != nil {
			return nil, err
		}
		return callResponse.Result, nil
	}
	err = repeatable.Run(s.AbstractService, runnerCaller, context, handler, callResponse.Extracted)
	return callResponse, err
}

func (s *service) callWebElement(context *endly.Context, request *WebElementCallRequest) (*WebElementCallResponse, error) {
	seleniumSession, err := s.session(context, request.SessionID)
	if err != nil {
		return nil, err
	}
	var response = &WebElementCallResponse{}
	err = request.Selector.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid selector: %v", err)
	}
	var selector = request.Selector

	element, err := seleniumSession.driver.FindElement(selector.By, selector.Value)
	if err != nil || element == nil {
		response.LookupError = fmt.Sprintf("failed to lookup element: %v %v", selector.By, selector.Value)
		return response, nil
	}
	callResponse, err := s.call(context, element, request.Call)
	if err != nil {
		return nil, err
	}
	response.Result = callResponse.Result
	return response, nil
}

func (s *service) open(context *endly.Context, request *OpenSessionRequest) (*OpenSessionResponse, error) {
	var response = &OpenSessionResponse{}
	seleniumSession, err := s.openSession(context, request)
	if err != nil {
		return nil, err
	}
	response.SessionID = seleniumSession.ID
	return response, nil
}

func (s *service) close(context *endly.Context, request *CloseSessionRequest) (*CloseSessionResponse, error) {
	var response = &CloseSessionResponse{
		SessionID: request.SessionID,
	}
	seleniumSession, err := s.session(context, request.SessionID)
	if err != nil {
		return nil, err
	}
	err = seleniumSession.driver.Close()
	return response, err
}

func (s *service) deployServerIfNeeded(context *endly.Context, request *StartRequest, target *url.Resource) (*StartResponse, error) {
	deploymentService, _ := context.Service(deploy.ServiceID)

	deployServerResponse := deploymentService.Run(context, &deploy.Request{
		Target:  target,
		AppName: Selenium,
		Version: request.Version,
	})
	if deployServerResponse.Error != "" {
		return nil, errors.New(deployServerResponse.Error)
	}
	deployGeckoDriverResponse := deploymentService.Run(context, &deploy.Request{
		Target:  target,
		AppName: GeckoDriver,
	})
	if deployGeckoDriverResponse.Error != "" {
		return nil, errors.New(deployGeckoDriverResponse.Error)
	}
	var response = &StartResponse{}
	response.GeckodriverPath = "/opt/selenium/geckodriver"
	response.ServerPath = "/opt/selenium/selenium-server-standalone.jar"
	return response, nil
}

func (s *service) setJdk(context *endly.Context, request *StartRequest) error {
	sdkService, _ := context.Service(sdk.ServiceID)
	response := sdkService.Run(context, &sdk.SetRequest{
		Sdk:     request.Sdk,
		Version: request.SdkVersion,
		Target:  request.Target,
	})

	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

func (s *service) stop(context *endly.Context, request *StopRequest) (*StopResponse, error) {
	var target, err = context.ExpandResource(request.Target)
	if err != nil {
		return nil, err
	}

	processService, _ := context.Service(process.ServiceID)
	serviceResponse := processService.Run(context, &process.StopAllRequest{
		Target: target,
		Input:  fmt.Sprintf("selenium-server-standalone.jar -port %v", toolbox.AsString(request.Port)),
	})
	if serviceResponse.Error != "" {
		return nil, errors.New(serviceResponse.Error)
	}
	return &StopResponse{}, nil
}

func (s *service) start(context *endly.Context, request *StartRequest) (*StartResponse, error) {
	var target, err = context.ExpandResource(request.Target)
	if err != nil {
		return nil, err
	}

	response, err := s.deployServerIfNeeded(context, request, target)
	if err != nil {
		return nil, err
	}
	err = s.setJdk(context, request)
	if err != nil {
		return nil, err
	}

	s.Run(context, &StopRequest{
		Target: target,
		Port:   request.Port,
	})
	processService, _ := context.Service(process.ServiceID)
	serviceResponse := processService.Run(context, &process.StartRequest{
		Command:         "java",
		Target:          target,
		Directory:       "/opt/selenium",
		Arguments:       []string{"-jar", fmt.Sprintf("-Dwebdriver.gecko.driver=%v", response.GeckodriverPath), "-jar", response.ServerPath, "-port", toolbox.AsString(request.Port)},
		ImmuneToHangups: true,
	})
	if serviceResponse.Error != "" {
		return nil, errors.New(serviceResponse.Error)
	}
	if processResponse, ok := serviceResponse.Response.(*process.StartResponse); ok && len(processResponse.Info) > 0 {
		response.Pid = processResponse.Info[0].Pid
	}
	return response, nil
}

func (s *service) session(context *endly.Context, sessionID string) (*Session, error) {
	sessions := Sessions(context)
	if seleniumSession, ok := sessions[sessionID]; ok {
		return seleniumSession, nil
	}
	return nil, fmt.Errorf("failed to lookup seleniun session id: %v, make sure you first run SeleniumOpenSessionRequest", sessionID)
}

func (s *service) openSession(context *endly.Context, request *OpenSessionRequest) (*Session, error) {
	resource, err := context.ExpandResource(request.RemoteSelenium)
	if err != nil {
		return nil, err
	}
	sessionID := resource.Host()
	sessions := Sessions(context)
	seleniumSession, ok := sessions[sessionID]
	if ok {
		if seleniumSession.Browser == request.Browser {
			return seleniumSession, nil
		}
		seleniumSession.driver.Close()
	} else {
		seleniumSession = &Session{
			ID:      sessionID,
			Browser: request.Browser,
		}
	}
	caps := selenium.Capabilities{"browserName": request.Browser}
	seleniumEndpoint := fmt.Sprintf("http://%v/wd/hub", resource.ParsedURL.Host)
	seleniumSession.driver, err = selenium.NewRemote(caps, seleniumEndpoint)

	if err != nil {
		return nil, err
	}
	sessions[sessionID] = seleniumSession
	context.Deffer(func() {
		seleniumSession.driver.Quit()
	})
	return seleniumSession, nil
}

const (
	seleniumServiceStartExample = `{
		"Target": {
			"URL": "127.0.0.1",
			"Credential": "${env.HOME}/.secret/localhost.json"
		},
		"Port": 8085,
		"Sdk": "jdk",
		"SdkVersion": "1.8",
		"Version": "3.4"
	}`

	seleniumServiceStopExample = `{
		"Target": {
			"URL": "file://127.0.0.1",
			"Credential": "${env.HOME}/.secret/localhost.json"
		},
		"Port": 8085
	}`

	seleniumServiceOpenSessionExample = ` {
		"Browser": "firefox",
		"RemoteSelenium": {
			"URL": "http://127.0.0.1:8085/"
		}
	}`

	seleniumServiceCloseExample = `{
"SessionID": "127.0.0.1:8085"
}`

	seleniumServiceCallDriverExample = `
 {
     "SessionID": "127.0.0.1:8085",
      "Calls": [
        {
          "Wait": null,
          "Method": "Get",
          "Parameters": [
            "http://127.0.0.1:8888/signin/"
          ]
        }
      ]
}
`

	seleniumServiceCallElementExample = ` {
 	"SessionID": "127.0.0.1:8085",
    "Selector": {
        "By": "xpath",
        "Value": "//SMALL[preceding-sibling::INPUT[@id='email']]",
        "Key": "email"
      },
      "Calls": [
        {
          "Method": "Text"
        }
      ]
    }`

	seleniumServiceRunAction = `{
  "SessionID": "127.0.0.1:8085",
  "Actions": [
    {
      "Calls": [
        {
          "Wait": null,
          "Method": "Get",
          "Parameters": [
            "http://127.0.0.1:8888/signin/"
          ]
        }
      ]
    },
    {
      "Selector": {
        "Value": "#email"
      },
      "Calls": [
        {
          "Method": "Clear"
        },
        {
          "Method": "SendKeys",
          "Parameters": [
            "xyz@wp.w"
          ]
        }
      ]
    },
    {
      "Selector": {
        "Value": "#password"
      },
      "Calls": [
        {
          "Method": "Clear"
        },
        {
          "Method": "SendKeys",
          "Parameters": [
            "pass1"
          ]
        }
      ]
    },
    {
      "Selector": {
        "Value": "#submit"
      },
      "Calls": [
        {
          "Wait": {
            "SleepTimeMs": 100
          },
          "Method": "Click"
        }
      ]
    },
    {
      "Selector": {
        "By": "xpath",
        "Value": "//SMALL[preceding-sibling::INPUT[@id='email']]",
        "Key": "email"
      },
      "Calls": [
        {
          "Method": "Text"
        }
      ]
    }
  ]
}
`
)

func (s *service) registerRoutes() {
	s.Register(&endly.ServiceActionRoute{
		Action: "start",
		RequestInfo: &endly.ActionInfo{
			Description: "start selenium server",
			Examples: []*endly.ExampleUseCase{
				{
					UseCase: "start server",
					Data:    seleniumServiceStartExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &StartRequest{}
		},
		ResponseProvider: func() interface{} {
			return &StartResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*StartRequest); ok {
				return s.start(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.Register(&endly.ServiceActionRoute{
		Action: "stop",
		RequestInfo: &endly.ActionInfo{
			Description: "stop selenium server",
			Examples: []*endly.ExampleUseCase{
				{
					UseCase: "stop server",
					Data:    seleniumServiceStopExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &StopRequest{}
		},
		ResponseProvider: func() interface{} {
			return &StopResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*StopRequest); ok {
				return s.stop(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.Register(&endly.ServiceActionRoute{
		Action: "open",
		RequestInfo: &endly.ActionInfo{
			Description: "open selenium session",
			Examples: []*endly.ExampleUseCase{
				{
					UseCase: "open session",
					Data:    seleniumServiceOpenSessionExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &OpenSessionRequest{}
		},
		ResponseProvider: func() interface{} {
			return &OpenSessionResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*OpenSessionRequest); ok {
				return s.open(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.Register(&endly.ServiceActionRoute{
		Action: "close",
		RequestInfo: &endly.ActionInfo{
			Description: "close selenium session",
			Examples: []*endly.ExampleUseCase{
				{
					UseCase: "close session",
					Data:    seleniumServiceCloseExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &CloseSessionRequest{}
		},
		ResponseProvider: func() interface{} {
			return &CloseSessionResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*CloseSessionRequest); ok {
				return s.close(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.Register(&endly.ServiceActionRoute{
		Action: "run",
		RequestInfo: &endly.ActionInfo{
			Description: "run selenium requests",
			Examples: []*endly.ExampleUseCase{
				{
					UseCase: "run",
					Data:    seleniumServiceRunAction,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &RunRequest{}
		},
		ResponseProvider: func() interface{} {
			return &RunResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*RunRequest); ok {
				return s.run(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.Register(&endly.ServiceActionRoute{
		Action: "call-driver",
		RequestInfo: &endly.ActionInfo{
			Description: "call proxies request to  github.com/tebeka/selenium web driver",
			Examples: []*endly.ExampleUseCase{
				{
					UseCase: "call driver",
					Data:    seleniumServiceCallDriverExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &WebDriverCallRequest{}
		},
		ResponseProvider: func() interface{} {
			return &ServiceCallResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*WebDriverCallRequest); ok {
				return s.callWebDriver(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.Register(&endly.ServiceActionRoute{
		Action: "call-element",
		RequestInfo: &endly.ActionInfo{
			Description: "find web element and proxy request",
			Examples: []*endly.ExampleUseCase{
				{
					UseCase: "web element call",
					Data:    seleniumServiceCallElementExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &WebElementCallRequest{}
		},
		ResponseProvider: func() interface{} {
			return &ServiceCallResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*WebElementCallRequest); ok {
				return s.callWebElement(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

}

//NewSeleniumService creates a new selenium service
func New() endly.Service {
	var result = &service{
		AbstractService: endly.NewAbstractService(ServiceID),
	}
	result.AbstractService.Service = result
	result.registerRoutes()
	return result
}
