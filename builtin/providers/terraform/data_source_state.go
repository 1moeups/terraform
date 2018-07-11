package terraform

import (
	"fmt"
	"log"

	"github.com/hashicorp/terraform/backend"
	backendinit "github.com/hashicorp/terraform/backend/init"
	"github.com/hashicorp/terraform/config/hcl2shim"
	"github.com/hashicorp/terraform/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

// turn error into diags
func dataSourceRemoteStateRead(d *cty.Value) (cty.Value, error) {
	newState := make(map[string]cty.Value)
	newState["backend"] = d.GetAttr("backend")

	backendType := d.GetAttr("backend").AsString()

	// Don't break people using the old _local syntax - but note warning above
	if backendType == "_local" {
		log.Println(`[INFO] Switching old (unsupported) backend "_local" to "local"`)
		backendType = "local"
	}

	// Create the client to access our remote state
	log.Printf("[DEBUG] Initializing remote state backend: %s", backendType)
	f := backendinit.Backend(backendType)
	if f == nil {
		return cty.NilVal, fmt.Errorf("Unknown backend type: %s", backendType)
	}
	b := f()

	config := d.GetAttr("config")
	newState["config"] = config

	schema := b.ConfigSchema()
	// Try to coerce the provided value into the desired configuration type.
	configVal, err := schema.CoerceValue(config)
	if err != nil {
		return cty.NilVal, fmt.Errorf("invalid %s backend configuration: %s", backendType, tfdiags.FormatError(err))
	}

	validateDiags := b.ValidateConfig(configVal)
	if validateDiags.HasErrors() {
		return cty.NilVal, validateDiags.Err()
	}
	configureDiags := b.Configure(configVal)
	if configureDiags.HasErrors() {
		return cty.NilVal, configureDiags.Err()
	}

	// environment is deprecated in favour of workspace.
	// If both keys are set workspace should win.
	var name string

	if d.Type().HasAttribute("environment") {
		newState["environment"] = d.GetAttr("environment")
		name = d.GetAttr("environment").AsString()
	}

	if d.Type().HasAttribute("workspace") {
		newState["workspace"] = d.GetAttr("workspace")
		ws := d.GetAttr("workspace").AsString()
		if ws != backend.DefaultStateName {
			name = ws
		}
	}

	state, err := b.State(name)
	if err != nil {
		return cty.NilVal, fmt.Errorf("error loading the remote state: %s", err)
	}
	if err := state.RefreshState(); err != nil {
		return cty.NilVal, err
	}

	outputMap := make(map[string]interface{})

	if d.Type().HasAttribute("defaults") {
		defaults := d.GetAttr("defaults")
		newState["defaults"] = d.GetAttr("defaults")
		it := defaults.ElementIterator()
		for it.Next() {
			k, v := it.Element()
			outputMap[k.AsString()] = v
		}
	}

	remoteState := state.State()
	if remoteState.Empty() {
		log.Println("[DEBUG] empty remote state")
	} else {
		for key, val := range remoteState.RootModule().Outputs {
			if val.Value != nil {
				outputMap[key] = val.Value
			}
		}
	}

	newState["outputs"] = hcl2shim.HCL2ValueFromConfigValue(outputMap)

	return cty.ObjectVal(newState), nil
}
