package heroku

import (
	"fmt"
	"log"

	"github.com/cyberdelia/heroku-go/v3"
	"github.com/hashicorp/terraform/helper/multierror"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
)

type herokuApplication struct {
	Name             string
	Region           string
	Stack            string
	GitURL           string
	WebURL           string
	OrganizationName string
	Locked           bool
}

// type application is used to store all the details of a heroku app
type application struct {
	Id string // Id of the resource

	App          *herokuApplication // The heroku application
	Client       *heroku.Service    // Client to interact with the heroku API
	Vars         map[string]string  // The vars on the application
	Organization bool               // is the application organization app
}

// Updates the application to have the latest from remote
func (a *application) Update() error {
	var errs []error
	var err error

	if !a.Organization {
		app, err := a.Client.AppInfo(a.Id)
		if err != nil {
			errs = append(errs, err)
		} else {
			a.App = &herokuApplication{}
			a.App.Name = app.Name
			a.App.Region = app.Region.Name
			a.App.Stack = app.Stack.Name
			a.App.GitURL = app.GitURL
			a.App.WebURL = app.WebURL
		}
	} else {
		app, err := a.Client.OrganizationAppInfo(a.Id)
		if err != nil {
			errs = append(errs, err)
		} else {
			// No inheritance between OrganizationApp and App is killing it :/
			a.App = &herokuApplication{}
			a.App.Name = app.Name
			a.App.Region = app.Region.Name
			a.App.Stack = app.Stack.Name
			a.App.GitURL = app.GitURL
			a.App.WebURL = app.WebURL
			if app.Organization != nil {
				a.App.OrganizationName = app.Organization.Name
			} else {
				log.Println("[DEBUG] Something is wrong - didn't get information about organization name, while the app is marked as being so")
			}
			a.App.Locked = app.Locked
		}
	}

	a.Vars, err = retrieveConfigVars(a.Id, a.Client)
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return &multierror.Error{Errors: errs}
	}

	return nil
}

func resourceHerokuApp() *schema.Resource {
	return &schema.Resource{
		Create: switchHerokuAppCreate,
		Read:   resourceHerokuAppRead,
		Update: resourceHerokuAppUpdate,
		Delete: resourceHerokuAppDelete,
		CreateInitialInstanceState: resourceHerokuAppCreateInitialInstanceState,

		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},

			"region": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"stack": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},

			"config_vars": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Schema{
					Type: schema.TypeMap,
				},
			},

			"all_config_vars": &schema.Schema{
				Type:     schema.TypeMap,
				Computed: true,
			},

			"git_url": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"web_url": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"heroku_hostname": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"organization": &schema.Schema{
				Description: "Name of Organization to create application in. Leave blank for personal apps.",
				Type:        schema.TypeList,
				Optional:    true,
				ForceNew:    true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},

						"locked": &schema.Schema{
							Type:     schema.TypeBool,
							Optional: true,
						},

						"personal": &schema.Schema{
							Type:     schema.TypeBool,
							Optional: true,
						},
					},
				},
			},
		},
	}
}

func isOrganizationApp(d *schema.ResourceData) bool {
	_, ok := d.GetOk("organization.0.name")
	return ok
}

func switchHerokuAppCreate(d *schema.ResourceData, meta interface{}) error {
	orgCount := d.Get("organization.#").(int)
	if orgCount > 1 {
		return fmt.Errorf("Error Creating Heroku App: Only 1 Heroku Organization is permitted")
	}

	if isOrganizationApp(d) {
		return resourceHerokuOrgAppCreate(d, meta)
	} else {
		return resourceHerokuAppCreate(d, meta)
	}
}

func resourceHerokuAppCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Service)

	// Build up our creation options
	opts := heroku.AppCreateOpts{}

	if v, ok := d.GetOk("name"); ok {
		vs := v.(string)
		log.Printf("[DEBUG] App name: %s", vs)
		opts.Name = &vs
	}
	if v, ok := d.GetOk("region"); ok {
		vs := v.(string)
		log.Printf("[DEBUG] App region: %s", vs)
		opts.Region = &vs
	}
	if v, ok := d.GetOk("stack"); ok {
		vs := v.(string)
		log.Printf("[DEBUG] App stack: %s", vs)
		opts.Stack = &vs
	}

	log.Printf("[DEBUG] Creating Heroku app...")
	a, err := client.AppCreate(opts)
	if err != nil {
		return err
	}

	d.SetId(a.Name)
	log.Printf("[INFO] App ID: %s", d.Id())

	if v, ok := d.GetOk("config_vars"); ok {
		err = updateConfigVars(d.Id(), client, nil, v.([]interface{}))
		if err != nil {
			return err
		}
	}

	return resourceHerokuAppRead(d, meta)
}

func resourceHerokuOrgAppCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Service)
	// Build up our creation options
	opts := heroku.OrganizationAppCreateOpts{}

	if v := d.Get("organization.0.name"); v != nil {
		vs := v.(string)
		log.Printf("[DEBUG] Organization name: %s", vs)
		opts.Organization = &vs
	}

	if v := d.Get("organization.0.personal"); v != nil {
		vs := v.(bool)
		log.Printf("[DEBUG] Organization Personal: %t", vs)
		opts.Personal = &vs
	}

	if v := d.Get("organization.0.locked"); v != nil {
		vs := v.(bool)
		log.Printf("[DEBUG] Organization locked: %t", vs)
		opts.Locked = &vs
	}

	if v := d.Get("name"); v != nil {
		vs := v.(string)
		log.Printf("[DEBUG] App name: %s", vs)
		opts.Name = &vs
	}
	if v, ok := d.GetOk("region"); ok {
		vs := v.(string)
		log.Printf("[DEBUG] App region: %s", vs)
		opts.Region = &vs
	}
	if v, ok := d.GetOk("stack"); ok {
		vs := v.(string)
		log.Printf("[DEBUG] App stack: %s", vs)
		opts.Stack = &vs
	}

	log.Printf("[DEBUG] Creating Heroku app...")
	a, err := client.OrganizationAppCreate(opts)
	if err != nil {
		return err
	}

	d.SetId(a.Name)
	log.Printf("[INFO] App ID: %s", d.Id())

	if v, ok := d.GetOk("config_vars"); ok {
		err = updateConfigVars(d.Id(), client, nil, v.([]interface{}))
		if err != nil {
			return err
		}
	}

	return resourceHerokuAppRead(d, meta)
}

func resourceHerokuAppRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Service)

	configVars := make(map[string]string)
	care := make(map[string]struct{})
	for _, v := range d.Get("config_vars").([]interface{}) {
		for k, _ := range v.(map[string]interface{}) {
			care[k] = struct{}{}
		}
	}

	_, organizationApp := d.GetOk("organization.0.name")
	// Only set the config_vars that we have set in the configuration.
	// The "all_config_vars" field has all of them.
	app, err := resourceHerokuAppRetrieve(d.Id(), organizationApp, client)
	if err != nil {
		return err
	}

	for k, v := range app.Vars {
		if _, ok := care[k]; ok {
			configVars[k] = v
		}
	}
	var configVarsValue []map[string]string
	if len(configVars) > 0 {
		configVarsValue = []map[string]string{configVars}
	}

	d.Set("name", app.App.Name)
	d.Set("stack", app.App.Stack)
	d.Set("region", app.App.Region)
	d.Set("git_url", app.App.GitURL)
	d.Set("web_url", app.App.WebURL)
	d.Set("config_vars", configVarsValue)
	d.Set("all_config_vars", app.Vars)
	if organizationApp {
		d.Set("organization.#", "1")
		d.Set("organization.0.name", app.App.OrganizationName)
		d.Set("organization.0.locked", app.App.Locked)
		d.Set("organization.0.private", false)
	}

	// We know that the hostname on heroku will be the name+herokuapp.com
	// You need this to do things like create DNS CNAME records
	d.Set("heroku_hostname", fmt.Sprintf("%s.herokuapp.com", app.App.Name))

	return nil
}

func resourceHerokuAppUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Service)

	// If name changed, update it
	if d.HasChange("name") {
		v := d.Get("name").(string)
		opts := heroku.AppUpdateOpts{
			Name: &v,
		}

		renamedApp, err := client.AppUpdate(d.Id(), opts)
		if err != nil {
			return err
		}

		// Store the new ID
		d.SetId(renamedApp.Name)
	}

	// If the config vars changed, then recalculate those
	if d.HasChange("config_vars") {
		o, n := d.GetChange("config_vars")
		if o == nil {
			o = []interface{}{}
		}
		if n == nil {
			n = []interface{}{}
		}

		err := updateConfigVars(
			d.Id(), client, o.([]interface{}), n.([]interface{}))
		if err != nil {
			return err
		}
	}

	return resourceHerokuAppRead(d, meta)
}

func falseOnNil(val interface{}) bool {
    if val == nil {
        return false
    }

    return val.(bool)
}

func resourceHerokuAppCreateInitialInstanceState(config *terraform.ResourceConfig, state *terraform.InstanceState, meta interface{}) (*terraform.InstanceState, error) {
	client := meta.(*heroku.Service)

	name, _ := config.Get("name")
	organizationMapV, organizationApp := config.Get("organization")
	app, err := resourceHerokuAppRetrieve(name.(string), organizationApp, client)
	if err != nil {
		return nil, err
	}
	state.ID = name.(string)
	if organizationApp {
		organizationMap := organizationMapV.([]map[string]interface{})[0]
		state.Attributes["organization.#"] = "1"
		state.Attributes["organization.0.name"] = organizationMap["name"].(string)
		state.Attributes["organization.0.locked"] = fmt.Sprintf("%v", falseOnNil(organizationMap["locked"]))
		state.Attributes["organization.0.private"] = fmt.Sprintf("%v", false)
	}

	state.Attributes["config_vars.#"] = "1"
	count := 0
	configArr, _ := config.Get("config_vars")
	configVars := configArr.([]map[string]interface{})
	for k, _ := range configVars[0] {
		v, ok := app.Vars[k]
		if ok {
			state.Attributes["config_vars.0."+k] = v
		}
		count += 1
	}
	state.Attributes["config_vars.0.#"] = fmt.Sprintf("%d", count)

	return state, nil
}

func resourceHerokuAppDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Service)

	log.Printf("[INFO] Deleting App: %s", d.Id())
	err := client.AppDelete(d.Id())
	if err != nil {
		return fmt.Errorf("Error deleting App: %s", err)
	}

	d.SetId("")
	return nil
}

func resourceHerokuAppRetrieve(id string, organization bool, client *heroku.Service) (*application, error) {
	app := application{Id: id, Client: client, Organization: organization}

	err := app.Update()

	if err != nil {
		return nil, fmt.Errorf("Error retrieving app: %s", err)
	}

	return &app, nil
}

func retrieveConfigVars(id string, client *heroku.Service) (map[string]string, error) {
	vars, err := client.ConfigVarInfo(id)

	if err != nil {
		return nil, err
	}

	return vars, nil
}

// Updates the config vars for from an expanded configuration.
func updateConfigVars(
	id string,
	client *heroku.Service,
	o []interface{},
	n []interface{}) error {
	vars := make(map[string]*string)

	for _, v := range o {
		for k, _ := range v.(map[string]interface{}) {
			vars[k] = nil
		}
	}
	for _, v := range n {
		for k, v := range v.(map[string]interface{}) {
			val := v.(string)
			vars[k] = &val
		}
	}

	log.Printf("[INFO] Updating config vars: *%#v", vars)
	if _, err := client.ConfigVarUpdate(id, vars); err != nil {
		return fmt.Errorf("Error updating config vars: %s", err)
	}

	return nil
}
