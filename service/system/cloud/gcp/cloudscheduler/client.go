package cloudscheduler

import (
	"fmt"
	"github.com/viant/endly"
	"github.com/viant/endly/service/system/cloud/gcp"
	"google.golang.org/api/cloudscheduler/v1beta1"
)

var clientKey = (*CtxClient)(nil)

// CtxClient represents context client
type CtxClient struct {
	*gcp.AbstractClient
	service *cloudscheduler.Service
}

func (s *CtxClient) SetService(service interface{}) error {
	var ok bool
	s.service, ok = service.(*cloudscheduler.Service)
	if !ok {
		return fmt.Errorf("unable to set service: %T", service)
	}
	return nil
}

func (s *CtxClient) Service() interface{} {
	return s.service
}

func InitRequest(context *endly.Context, rawRequest map[string]interface{}) error {
	config, err := gcp.InitCredentials(context, rawRequest)
	if err != nil {
		return err
	}
	client, err := getClient(context)
	if err != nil {
		return err
	}
	gcp.UpdateActionRequest(rawRequest, config, client)
	return nil
}

func getClient(context *endly.Context) (gcp.CtxClient, error) {
	return GetClient(context)
}

func GetClient(context *endly.Context) (*CtxClient, error) {
	client := &CtxClient{
		AbstractClient: &gcp.AbstractClient{},
	}
	err := gcp.GetClient(context, cloudscheduler.NewService, clientKey, &client, cloudscheduler.CloudPlatformScope)
	return client, err
}
