package coremgt

import (
	"fmt"
	"regexp"
	"strings"

	goversion "github.com/hashicorp/go-version"

	"github.com/forj-oss/forjj-modules/trace"
)

const (
	pluginType = "plugin"
)

// Plugin describe details on Plugin element.
// By default, it helps reading the MANIFEST.MF from java as a yaml file, cleaned before.
type Plugin struct {
	Version        string `yaml:"Plugin-Version"`
	ExtensionName  string `yaml:"Extension-Name"`
	ShortName      string `yaml:"Short-Name"`
	JenkinsVersion string `yaml:"Jenkins-Version"`
	LongName       string `yaml:"Long-Name"`
	Dependencies   string `yaml:"Plugin-Dependencies"`
	Description    string `yaml:"Specification-Title"`
	checkSumSha256 string 
	rules          map[string]goversion.Constraints
	fixed          bool     // true if a constraint force a version
	parents        Elements // List of parent Elements dependencies
	dependencies   Elements // List of Elements dependencies
}

// String return the string representation of the plugin
func (p *Plugin) String() string {
	if p == nil {
		return "nil"
	}
	ruleShown := make([]string, len(p.rules))
	index := 0
	constraints := ""
	for ruleName, rule := range p.rules {
		ruleShown[index] = fmt.Sprintf("%s (%s)", ruleName, rule.String())
		index++
	}

	if index > 0 {
		constraints = fmt.Sprintf(" (constraints: %s)", strings.Join(ruleShown, ", "))
	}

	fixed := ""
	if p.fixed {
		fixed = "FIXED:"
	}

	return fmt.Sprintf("(%p)%s:%s %s%s%s", p, pluginType, p.ExtensionName, fixed, p.Version, constraints)
}

// GetVersion return the plugin Version struct.
func (p *Plugin) GetVersion() (ret VersionStruct, err error) {
	err = ret.Set(p.Version)
	return
}

// GetVersionString return the plugin value as string.
func (p *Plugin) GetVersionString() string {
	return p.Version
}

// NewPlugin return a plugin object
func NewPlugin() (ret *Plugin) {
	ret = new(Plugin)
	ret.rules = make(map[string]goversion.Constraints)
	ret.parents = make(map[string]Element)
	ret.dependencies = make(map[string]Element)
	return
}

// SetFrom set data from an array of fields
// If the version is given, it will be interpreted as a constraint
func (p *Plugin) SetFrom(fields ...string) (err error) {
	err = p.setFrom(fields...)
	if err != nil {
		return
	}

	if p.Version != "" { // If version is given, it will be an equal constraint, except if it is already a constraint
		var constraints goversion.Constraints
		constraints, err = goversion.NewConstraint(p.Version)
		if err != nil {
			err = fmt.Errorf("Version constraints are invalid. %s", err)
			return
		}

		constraintPiecesRe, _ := regexp.Compile(`^([<>=!~]*)(.*)$`)
		constraintPieces := constraintPiecesRe.FindStringSubmatch(p.Version)
		ruleName := "GreaterOrEqualTo"
		if constraintPieces != nil {
			if constraintPieces[1] != "" {
				// Remove the constraints rule piece of the verison string
				p.Version = constraintPieces[2]
			}
			if !p.fixed {
				if constraintPieces[1] == "" || constraintPieces[1] == "=" {
					// Version fixing
					p.fixed = true
					ruleName = "FixedTo"
					p.rules = make(map[string]goversion.Constraints)
					constraints, _ = goversion.NewConstraint("<=" + p.Version)
				}
			} else {
				if (constraintPieces[1] == "" || constraintPieces[1] == "=") && constraintPieces[2] != p.Version {
					err = fmt.Errorf("%s has been pinned twice to 2 different versions. '%s' vs '%s'", p.ExtensionName, p.Version, constraintPieces[2])
				}
				return
			}
		}

		p.rules[ruleName] = constraints

	}
	return
}

// setFrom set data from an array of fields, with no constraints
func (p *Plugin) setFrom(fields ...string) (err error) {
	fieldsSize := len(fields)
	if fieldsSize < 2 {
		err = fmt.Errorf("Invalid data type. Requires type (field 1) as '%s' and plugin name (field 2)", pluginType)
		return
	}
	if fields[0] != pluginType {
		err = fmt.Errorf("Invalid data type. Must be '%s'", pluginType)
		return
	}
	p.ExtensionName = fields[1]
	if fieldsSize >= 3 {
		p.Version = fields[2]
	}
	return
}

// CompleteFromContext update the plugin information from repo DB if found
func (p *Plugin) CompleteFromContext(context *ElementsType) (err error) {
	if p == nil || context == nil || context.ref == nil {
		return
	}

	refPlugin, found := context.ref.Get(p.ExtensionName, p.Version)
	if !found {
		return
	}

	p.ShortName = refPlugin.Name
	p.JenkinsVersion = refPlugin.JenkinsVersion
	p.Description = refPlugin.Description
	p.checkSumSha256 = refPlugin.Sha256Version

	return
}

// GetType return the internal type string
func (p *Plugin) GetType() string {
	return pluginType
}

// Name return the Name property
func (p *Plugin) Name() string {
	return p.ExtensionName
}

// ChainElement load plugins dependency tree from the repo
//
// The constraint is added as expected, but the version is the highest possible. (latest)
func (p *Plugin) ChainElement(context *ElementsType) (ret *ElementsType, _ error) {
	if p == nil {
		return
	}

	if context.ref == nil {
		return nil, fmt.Errorf("Missing repository loaded. Load it to the ElementsType list or call ElementsType.NoRecursivChain()")
	}
	refPlugin, found := context.ref.Get(p.ExtensionName)
	if !found {
		return nil, fmt.Errorf("Plugin '%s' not found in the public repository", p.Name())
	}

	ret = NewElementsType()
	ret.AddSupport(pluginType)
	ret.noRecursiveChain()
	ret.SetRepository(context.ref)

	for _, dep := range refPlugin.Dependencies {
		refDepPlugin, found := context.ref.Get(dep.Name)
		if !found {
			gotrace.Warning("The plugin '%s' has a dependent plugin '%s' not found in the public repository. Ignored.", p.Name(), dep.Name)
			continue
		}
		if dep.Optional {
			continue
		}
		plugin := NewPlugin()
		plugin.SetFrom(pluginType, dep.Name, ">="+dep.Version)
		plugin.Version = refDepPlugin.Version //
		ret.AddElement(plugin)
	}
	return
}

// Merge execute a merge between 2 plugins and keep the one corresponding to the constraint given
// It is based on 3 policies: choose oldest, keep existing and choose newest
func (p *Plugin) Merge(context *ElementsType, element Element, policy int) (updated bool, err error) {
	if p == nil {
		return
	}
	if p.fixed { // The plugin version is fixed (= constraint)
		return
	}

	origVersion, _ := p.GetVersion()
	newPlugin, ok := element.(*Plugin)
	if !ok {
		err = fmt.Errorf("Plugin merge support only plugins element type")
		return
	}
	newVersion, _ := newPlugin.GetVersion()

	// No version to merge, so exit.
	if origVersion.Get() == nil && newVersion.Get() == nil {
		return
	}

	// If current plugin has no version, get it from the new one and exit
	if origVersion.Get() == nil {
		p.Version = newPlugin.Version
		p.rules = newPlugin.rules
		updated = true
		return
	}

	// If no version to merge from, then exit
	if newVersion.Get() == nil {
		return
	}

	switch policy {
	case oldestPolicy:
		if origVersion.Get().GreaterThan(newVersion.Get()) {
			p.Version = newPlugin.Version
			p.rules = newPlugin.rules
			updated = true
		}
	case keepPolicy: // No merge
	case newestPolicy:
		if origVersion.Get().LessThan(newVersion.Get()) {
			p.Version = newPlugin.Version
			p.rules = newPlugin.rules
			updated = true
		}
	}

	return
}

// IsFixed indicates if the plugin version is fixed.
func (p *Plugin) IsFixed() (_ bool) {
	if p == nil {
		return
	}
	return p.fixed
}

// SetVersionConstraintFromDepConstraint add a constraint to match the
// dependency version constraints on plugins parent dependencies
//
// This function check parent plugin of the dependency given to change version constraint if needed.
func (p *Plugin) SetVersionConstraintFromDepConstraint(context *ElementsType, depElement Element) (err error) {

	depPlugin, ok := depElement.(*Plugin)
	if !ok {
		return
	}

	// Loop on all versions from latest to oldest
	for _, version := range context.ref.GetOrderedVersions(p.ExtensionName) {

		// Get the plugin dependencies for this specific version from updates.
		refPlugin, _ := context.ref.Get(p.ExtensionName, version.Original())

		// Get the required plugin version for this plugin (dependency)
		depPluginVersion := refPlugin.Dependencies.GetVersion(depPlugin.ExtensionName)

		// Ignore if the version is currently higher than minimum requested.
		if p.Version != "" {
			if v, _ := goversion.NewVersion(p.Version); version.GreaterThan(v) {
				gotrace.TraceLevel(2, "%s not minimum version. %s ignored.", p.Name(), version.Original())
				continue
			}
		}
		gotrace.TraceLevel(1, "Checking parent %s %s with %s dependency", p.Name(), version.Original(), depElement)
		// If a dependency is found, check version candidature.
		if depPluginVersion != nil {
			// Check if the plugin dependency rule match the requirement.
			candidate := depElement.IsVersionCandidate(depPluginVersion)
			gotrace.TraceLevel(2, "From %s %s, %s minimum requirement is %s. Is it validated? %t", p.Name(), version.Original(), depElement.Name(), depPluginVersion.Original(), candidate)
			if !candidate {
				continue // Go to the earlier version
			}
		}

		// Ignore if the version is currently higher than minimum requested.
		if p.Version != "" {
			if v, _ := goversion.NewVersion(p.Version); version.Equal(v) {
				gotrace.TraceLevel(2, "Respect current rule. parent %s ignored.", p.ExtensionName)
				return
			}
		}

		// Define a constraint that this plugin cannot be higher than this version.
		constraint, _ := goversion.NewConstraint("<=" + version.Original())

		// Set or replace the LessOrEqualTo contraint
		gotrace.TraceLevel(0, "%s is downgraded to %s due to %s.", p, version.Original(), depPlugin)
		delete(p.rules, "GreaterOrEqualTo")
		p.rules["LessOrEqualTo"] = constraint
		p.Version = version.Original()
		p.updateDependenciesRelations(context, refPlugin)

		gotrace.TraceLevel(2, "New constraints %s", p)

		// Do this work with the new parent version to parent of this plugin
		for _, elementToConstrain := range p.parents {
			err = elementToConstrain.SetVersionConstraintFromDepConstraint(context, p)
			if err != nil {
				return
			}
		}
		return
	}
	return fmt.Errorf("Unable to find a proper version of %s which match the dep need to %s", p, depElement)
}

// updateDependenciesRelations of the current plugins with a new refplugin given
// It will add new one and remove old one
func (p *Plugin) updateDependenciesRelations(context *ElementsType, refPlugin *RepositoryPlugin) {
	treatedPlugins := make(map[string]bool)

	for _, dependency := range refPlugin.Dependencies {
		if _, found := p.dependencies[dependency.Name]; found {
			treatedPlugins[dependency.Name] = true
			continue
		} else {
			context.Add(pluginType, dependency.Name, ">="+dependency.Version)
			treatedPlugins[dependency.Name] = true
		}
	}

	for depName, depElement := range p.dependencies {
		_, found := treatedPlugins[depName]
		// We found a dependency which was already treated
		if found {
			continue
		}

		// Old dependency to remove
		p.RemoveDependencyTo(depElement)

		if len(depElement.GetParents()) == 0 {
			context.DeleteElement(depElement)
		}
	}
}

// IsVersionCandidate return true if version given respect the rule
func (p *Plugin) IsVersionCandidate(version *goversion.Version) bool {
	for _, rule := range p.rules {
		if !rule.Check(version) {
			return false
		}
	}
	return true
}

// DefineLatestPossibleVersion check on version history which latest version is possible from version rules given.
func (p *Plugin) DefineLatestPossibleVersion(context *ElementsType) (_ error) {
	Versions := context.ref.GetOrderedVersions(p.ExtensionName)
	for _, version := range Versions {
		if p.IsVersionCandidate(version) {
			p.Version = version.Original()
			if gotrace.IsDebugMode() {
				fmt.Printf("*** Selected version %s for %s \n", p.Version, p.ExtensionName)
			}
			return
		}
	}
	return fmt.Errorf("Unable to find a latest version for %s which respect version rules", p)
}

// AsNewPluginsStatusDetails return a PluginsStatusDetails object from this plugin, considered as new.
func (p *Plugin) AsNewPluginsStatusDetails(context *ElementsType) (sd *pluginsStatusDetails) {
	plugin, found := context.ref.Get(p.ExtensionName, p.Version)
	if !found {
		return
	}
	sd = newPluginsStatusDetails()
	sd.name = p.ExtensionName
	sd.title = plugin.Title
	version := VersionStruct{}

  if err := version.Set(plugin.Version); err != nil {
		gotrace.Error("New version struct for %s '%s' invalid. %s", sd.name, plugin.Version, err)
		return nil
	}

	sd.newVersion = version
	sd.oldVersion = VersionStruct{}
	sd.oldVersion.Set("new")
	if latest, found := context.ref.Get(p.ExtensionName); found {
		sd.latest = (latest.Version == plugin.Version)
		gotrace.TraceLevel(1, "%s latest %t", p.ExtensionName, sd.latest)
	} else {
		gotrace.TraceLevel(1, "Unable to find %s", p.ExtensionName)
	}
	return
}

// AsNewGrooviesStatusDetails to be removed.
func (p *Plugin) AsNewGrooviesStatusDetails(context *ElementsType) (sd *GroovyStatusDetails) {
	return
}

/******* Dependency management ************/

// GetParents return the list of plugins which depends on this plugin.
func (p *Plugin) GetParents() Elements {
	return p.parents
}

// GetDependenciesFromContext return the list of plugins depedencies required by this plugin.
// Required when elements were simply listed by ChainElements to update their dependencies
func (p *Plugin) GetDependenciesFromContext(context *ElementsType) Elements {
	if p == nil {
		return nil
	}

	pluginRef, found := context.ref.Get(p.ExtensionName, p.Version)
	if found {
		for _, pluginDep := range pluginRef.Dependencies {
			newPluginDep := NewPlugin()
			newPluginDep.ExtensionName = pluginDep.Name
			newPluginDep.Version = pluginDep.Version
			p.dependencies[pluginDep.Name] = newPluginDep
		}
	}
	return p.GetDependencies()
}

// GetDependencies return the list of plugins depedencies required by this plugin.
func (p *Plugin) GetDependencies() Elements {
	return p.dependencies
}

// RemoveDependencyTo remove a bi-directionnel dependency
func (p *Plugin) RemoveDependencyTo(depElement Element) {
	depPlugin := depElement.(*Plugin)
	delete(depPlugin.parents, p.Name())
	delete(p.dependencies, depPlugin.Name())
}

// AddDependencyTo creates the bi-directionnel dependency
func (p *Plugin) AddDependencyTo(depElement Element) {
	depPlugin := depElement.(*Plugin)
	// Add the dependency of current plugin to the other Element.
	// in short, p requires depElement
	p.dependencies[depPlugin.ExtensionName] = depPlugin

	// Define the parent dependency to p
	// In short, depPlugin is required by p
	depPlugin.parents[p.ExtensionName] = p
}
