// flatprops.go - Decoding of flat ARM objects for the OSIRIS JSON
// Microsoft Azure producer.
//
// ARM returns resource-specific fields under a "properties" object, and
// several structs in client.go model that nesting. Many az list
// commands flatten it instead: the same fields appear at the top level
// of each object and there is no "properties" key. A struct expecting
// the nested form then decodes those fields to nothing, silently.
//
// Every struct with a Properties pointer implements json.Unmarshaler
// here: it decodes normally and, only when the object has no
// "properties" key, decodes the object itself into Properties as well.
// Objects in the nested form are unaffected.
// The fallback never fails a decode: a field whose JSON type does no
// match is skipped as encoding/json does, and an object carrying none
// of the fields leaves Properties nil.

package azure

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

// flatProperties sets *props from the top level of data when data is a
// JSON object without a "properties" key (matched case-insensitively,
// as encoding/json matches field names) and *props is still nil.
func flatProperties[P any](data []byte, props **P) {
	if *props != nil {
		return
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(data, &keys) != nil {
		return
	}
	for k := range keys {
		if strings.EqualFold(k, "properties") {
			return
		}
	}
	var p P
	if err := json.Unmarshal(data, &p); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return
		}
	}
	if reflect.ValueOf(p).IsZero() {
		return
	}
	*props = &p
}

// UnmarshalJSON decodes a AKSCluster in nested or flat ARM form.
func (v *AKSCluster) UnmarshalJSON(data []byte) error {
	type plain AKSCluster
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a APIMService in nested or flat ARM form.
func (v *APIMService) UnmarshalJSON(data []byte) error {
	type plain APIMService
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a ApplicationInsights in nested or flat
// ARM form.
func (v *ApplicationInsights) UnmarshalJSON(data []byte) error {
	type plain ApplicationInsights
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a ArcMachine in nested or flat ARM form.
func (v *ArcMachine) UnmarshalJSON(data []byte) error {
	type plain ArcMachine
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a AutomationAccount in nested or flat ARM form.
func (v *AutomationAccount) UnmarshalJSON(data []byte) error {
	type plain AutomationAccount
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a AutoscaleSetting in nested or flat ARM form.
func (v *AutoscaleSetting) UnmarshalJSON(data []byte) error {
	type plain AutoscaleSetting
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a azVMSSIPConfig in nested or flat ARM form.
func (v *azVMSSIPConfig) UnmarshalJSON(data []byte) error {
	type plain azVMSSIPConfig
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a azVMSSNICConfig in nested or flat ARM form.
func (v *azVMSSNICConfig) UnmarshalJSON(data []byte) error {
	type plain azVMSSNICConfig
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a BackupInstance in nested or flat ARM form.
func (v *BackupInstance) UnmarshalJSON(data []byte) error {
	type plain BackupInstance
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a BackupProtectedItem in nested or flat
// ARM form.
func (v *BackupProtectedItem) UnmarshalJSON(data []byte) error {
	type plain BackupProtectedItem
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a BackupVault in nested or flat ARM form.
func (v *BackupVault) UnmarshalJSON(data []byte) error {
	type plain BackupVault
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a CommunicationService in nested or flat
// ARM form.
func (v *CommunicationService) UnmarshalJSON(data []byte) error {
	type plain CommunicationService
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a ContainerApp in nested or flat ARM form.
func (v *ContainerApp) UnmarshalJSON(data []byte) error {
	type plain ContainerApp
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a ContainerAppEnvironment in nested or flat
// ARM form.
func (v *ContainerAppEnvironment) UnmarshalJSON(data []byte) error {
	type plain ContainerAppEnvironment
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a ContainerGroup in nested or flat ARM form.
func (v *ContainerGroup) UnmarshalJSON(data []byte) error {
	type plain ContainerGroup
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a CosmosAccount in nested or flat ARM form.
func (v *CosmosAccount) UnmarshalJSON(data []byte) error {
	type plain CosmosAccount
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a DataCollectionEndpoint in nested or
// flat ARM form.
func (v *DataCollectionEndpoint) UnmarshalJSON(data []byte) error {
	type plain DataCollectionEndpoint
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a DataCollectionRule in nested or
// flat ARM form.
func (v *DataCollectionRule) UnmarshalJSON(data []byte) error {
	type plain DataCollectionRule
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a DataFactory in nested or flat ARM form.
func (v *DataFactory) UnmarshalJSON(data []byte) error {
	type plain DataFactory
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a DNSForwardingRuleset in nested or
// flat ARM form.
func (v *DNSForwardingRuleset) UnmarshalJSON(data []byte) error {
	type plain DNSForwardingRuleset
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a DNSPrivateResolver in nested or flat ARM form.
func (v *DNSPrivateResolver) UnmarshalJSON(data []byte) error {
	type plain DNSPrivateResolver
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a EmailDomain in nested or flat ARM form.
func (v *EmailDomain) UnmarshalJSON(data []byte) error {
	type plain EmailDomain
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a EmailService in nested or flat ARM form.
func (v *EmailService) UnmarshalJSON(data []byte) error {
	type plain EmailService
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a EventGridSystemTopic in nested or
// flat ARM form.
func (v *EventGridSystemTopic) UnmarshalJSON(data []byte) error {
	type plain EventGridSystemTopic
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a EventHubsNamespace in nested or
// flat ARM form.
func (v *EventHubsNamespace) UnmarshalJSON(data []byte) error {
	type plain EventHubsNamespace
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a FrontDoorProfile in nested or
// flat ARM form.
func (v *FrontDoorProfile) UnmarshalJSON(data []byte) error {
	type plain FrontDoorProfile
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a KeyVault in nested or flat ARM form.
func (v *KeyVault) UnmarshalJSON(data []byte) error {
	type plain KeyVault
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a LogicAPIConnection in nested or flat ARM form.
func (v *LogicAPIConnection) UnmarshalJSON(data []byte) error {
	type plain LogicAPIConnection
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a LogicWorkflow in nested or flat ARM form.
func (v *LogicWorkflow) UnmarshalJSON(data []byte) error {
	type plain LogicWorkflow
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a MySQLServer in nested or flat ARM form.
func (v *MySQLServer) UnmarshalJSON(data []byte) error {
	type plain MySQLServer
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a PostgreSQLServer in nested or flat ARM form.
func (v *PostgreSQLServer) UnmarshalJSON(data []byte) error {
	type plain PostgreSQLServer
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a RecoveryServicesVault in nested or
// flat ARM form.
func (v *RecoveryServicesVault) UnmarshalJSON(data []byte) error {
	type plain RecoveryServicesVault
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a ServiceBusNamespace in nested or
// flat ARM form.
func (v *ServiceBusNamespace) UnmarshalJSON(data []byte) error {
	type plain ServiceBusNamespace
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a SQLDatabase in nested or flat ARM form.
func (v *SQLDatabase) UnmarshalJSON(data []byte) error {
	type plain SQLDatabase
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a SQLElasticPool in nested or flat ARM form.
func (v *SQLElasticPool) UnmarshalJSON(data []byte) error {
	type plain SQLElasticPool
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a SQLManagedInstance in nested or
// flat ARM form.
func (v *SQLManagedInstance) UnmarshalJSON(data []byte) error {
	type plain SQLManagedInstance
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a SQLMIDatabase in nested or flat ARM form.
func (v *SQLMIDatabase) UnmarshalJSON(data []byte) error {
	type plain SQLMIDatabase
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a SQLServer in nested or flat ARM form.
func (v *SQLServer) UnmarshalJSON(data []byte) error {
	type plain SQLServer
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a SQLVirtualMachine in nested or flat ARM form.
func (v *SQLVirtualMachine) UnmarshalJSON(data []byte) error {
	type plain SQLVirtualMachine
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a StreamAnalyticsJob in nested or
// flat ARM form.
func (v *StreamAnalyticsJob) UnmarshalJSON(data []byte) error {
	type plain StreamAnalyticsJob
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a SynapseWorkspace in nested or
// flat ARM form.
func (v *SynapseWorkspace) UnmarshalJSON(data []byte) error {
	type plain SynapseWorkspace
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a VMSS in nested or flat ARM form.
func (v *VMSS) UnmarshalJSON(data []byte) error {
	type plain VMSS
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}

// UnmarshalJSON decodes a SQLFailoverGroup in nested or flat ARM form.
func (v *SQLFailoverGroup) UnmarshalJSON(data []byte) error {
	type plain SQLFailoverGroup
	if err := json.Unmarshal(data, (*plain)(v)); err != nil {
		return err
	}
	flatProperties(data, &v.Properties)
	return nil
}
