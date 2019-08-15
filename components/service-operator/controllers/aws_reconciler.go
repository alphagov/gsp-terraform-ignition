/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	"github.com/alphagov/gsp/components/service-operator/internal"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	internalaws "github.com/alphagov/gsp/components/service-operator/internal/aws"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	goformation "github.com/awslabs/goformation/cloudformation"
)

type CloudFormation interface {
	Template(string) *goformation.Template
	Parameters() ([]*cloudformation.Parameter, error)
}

// AWSReconciler reconciles an AWS object
type AWSReconciler struct {
	Log            logr.Logger
	ClusterName    string
	ResourceName   string
	CloudFormation CloudFormation
}

var (
	nonUpdatable = []string{
		cloudformation.StackStatusCreateInProgress,
		cloudformation.StackStatusRollbackInProgress,
		cloudformation.StackStatusDeleteInProgress,
		cloudformation.StackStatusUpdateInProgress,
		cloudformation.StackStatusUpdateCompleteCleanupInProgress,
		cloudformation.StackStatusUpdateRollbackInProgress,
		cloudformation.StackStatusUpdateRollbackCompleteCleanupInProgress,
		cloudformation.StackStatusReviewInProgress,
		cloudformation.StackStatusDeleteComplete,
	}
)

func (r *AWSReconciler) Reconcile(ctx context.Context, req ctrl.Request, deleting bool) (Action, internalaws.StackData, error) {
	stackName := fmt.Sprintf("%s-%s-%s-%s-%s", r.ClusterName, "gsp-service-operator", r.ResourceName, req.NamespacedName.Namespace, req.NamespacedName.Name)
	// secretName := coalesceString(postgres.Spec.Secret, postgres.Name)

	log := r.Log.WithValues("aws", req.NamespacedName)

	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	// metadata := ec2metadata.New(sess)
	// awsRegion, err := metadata.Region()
	// if err != nil {
	// 	return ctrl.Result{}, fmt.Errorf("unable to get AWS region from metdata: %s", err)
	// }
	awsRegion := "eu-west-2"
	sess.Config.Region = aws.String(awsRegion)

	svc := cloudformation.New(sess, aws.NewConfig())

	stackData, stackExists := r.getCloudFormationStackStatus(svc, stackName)

	if deleting {
		// The resource needs deleting
		if !stackExists || stackData.Status == cloudformation.StackStatusDeleteComplete {
			return Delete, stackData, nil
		}

		if stackData.Status == cloudformation.StackStatusDeleteInProgress {
			return Retry, stackData, nil
		}

		return Retry, stackData, r.deleteCloudFormationStack(svc, stackName, log)
	}

	yaml, err := r.CloudFormation.Template(stackName).YAML()
	if err != nil {
		return Retry, stackData, fmt.Errorf("error serialising template: %s", err)
	}

	if !stackExists { // create
		return Create, stackData, r.createCloudFormationStack(yaml, svc, stackName, log)
	} else if !internal.IsInList(stackData.Status, nonUpdatable...) { // update
		return Update, stackData, r.updateCloudFormationStack(yaml, svc, stackName, log)
	}

	return Retry, stackData, nil
}

type stackExists bool

func (r *AWSReconciler) getCloudFormationStackStatus(svc *cloudformation.CloudFormation, stackName string) (internalaws.StackData, stackExists) {
	describeOutput, err := svc.DescribeStacks(&cloudformation.DescribeStacksInput{StackName: aws.String(stackName)})
	if err != nil {
		return internalaws.StackData{}, false
	}
	return internalaws.StackData{
		ID:     *describeOutput.Stacks[0].StackId,
		Name:   stackName,
		Status: *describeOutput.Stacks[0].StackStatus,
		Reason: "NoReasonGiven",

		Outputs: describeOutput.Stacks[0].Outputs,
	}, true
}

func (r *AWSReconciler) createCloudFormationStack(yaml []byte, svc *cloudformation.CloudFormation, stackName string, log logr.Logger) error {
	log.V(1).Info("creating stack...", "stackName", stackName)

	params, err := r.CloudFormation.Parameters()
	if err != nil {
		return fmt.Errorf("error creating parameters: %s", err)
	}

	_, err = svc.CreateStack(&cloudformation.CreateStackInput{
		TemplateBody: aws.String(string(yaml)),
		StackName:    aws.String(stackName),
		Parameters:   params,
	})
	if err != nil {
		return fmt.Errorf("error creating stack: %s", err)
	}

	// TODO: create Secret

	return nil
}

func (r *AWSReconciler) updateCloudFormationStack(yaml []byte, svc *cloudformation.CloudFormation, stackName string, log logr.Logger) error {
	log.V(1).Info("updating stack...", "stackName", stackName)

	params, err := r.CloudFormation.Parameters()
	if err != nil {
		return fmt.Errorf("error creating parameters: %s", err)
	}

	_, err = svc.UpdateStack(&cloudformation.UpdateStackInput{
		TemplateBody: aws.String(string(yaml)),
		StackName:    aws.String(stackName),
		Parameters:   params,
	})
	if err != nil {
		return fmt.Errorf("error updating stack: %s", err)
	}

	return nil
}

func (r *AWSReconciler) deleteCloudFormationStack(svc *cloudformation.CloudFormation, stackName string, log logr.Logger) error {
	log.V(1).Info("deleting stack...", "stackName", stackName)
	_, err := svc.DeleteStack(&cloudformation.DeleteStackInput{StackName: aws.String(stackName)})
	// TODO: delete Secret
	return err
}