package billing

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

var taskQueue = "billing"

//encore:service
type Service struct {
	temporalClient client.Client
	temporalWorker worker.Worker
}

func initService() (*Service, error) {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return nil, fmt.Errorf("error creating temporal client: %w", err)
	}

	w := worker.New(c, taskQueue, worker.Options{})

	w.RegisterWorkflow(BillWorkflow)
	w.RegisterActivity(ChargeLineItemActivity)

	if err := w.Start(); err != nil {
		c.Close()
		return nil, fmt.Errorf("error starting termporal worker: %w", err)
	}
	return &Service{temporalClient: c, temporalWorker: w}, nil
}

func (s *Service) Shutdown(context.Context) {
	s.temporalWorker.Stop()
	s.temporalClient.Close()
}
