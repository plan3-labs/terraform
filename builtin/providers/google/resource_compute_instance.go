package google

import (
	"fmt"
	"log"
	"time"

	"code.google.com/p/google-api-go-client/compute/v1"
	"code.google.com/p/google-api-go-client/googleapi"
	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceComputeInstance() *schema.Resource {
	return &schema.Resource{
		Create: resourceComputeInstanceCreate,
		Read:   resourceComputeInstanceRead,
		Update: resourceComputeInstanceUpdate,
		Delete: resourceComputeInstanceDelete,

		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"machine_type": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"zone": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"disk": &schema.Schema{
				Type:     schema.TypeList,
				Required: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						// TODO(mitchellh): one of image or disk is required

						"disk": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
							ForceNew: true,
						},

						"image": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
							ForceNew: true,
						},

						"type": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
							ForceNew: true,
						},

						"auto_delete": &schema.Schema{
							Type:     schema.TypeBool,
							Optional: true,
							ForceNew: true,
						},
					},
				},
			},

			"network_interface": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"network": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
							ForceNew: true,
						},

						"name": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},

						"address": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},

						"access_config": &schema.Schema{
							Type:     schema.TypeList,
							Optional: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"nat_ip": &schema.Schema{
										Type: schema.TypeString,
										Computed: true,
										Optional: true,
									},
								},
							},
						},
					},
				},
			},

			"network": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"source": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
							ForceNew: true,
						},

						"address": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
							ForceNew: true,
						},

						"name": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},

						"internal_address": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},

						"external_address": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
			},

			"can_ip_forward": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
				ForceNew: true,
			},

			"metadata": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Schema{
					Type: schema.TypeMap,
				},
			},

			"service_account": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"email": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
							ForceNew: true,
						},

						"scopes": &schema.Schema{
							Type:     schema.TypeList,
							Required: true,
							ForceNew: true,
							Elem: &schema.Schema{
								Type: schema.TypeString,
								StateFunc: func(v interface{}) string {
									return canonicalizeServiceScope(v.(string))
								},
							},
						},
					},
				},
			},

			"tags": &schema.Schema{
				Type:     schema.TypeSet,
				Optional: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set: func(v interface{}) int {
					return hashcode.String(v.(string))
				},
			},

			"metadata_fingerprint": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"tags_fingerprint": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"self_link": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceOperationWaitZone(
	config *Config, op *compute.Operation, zone string, activity string) error {

	w := &OperationWaiter{
		Service: config.clientCompute,
		Op:      op,
		Project: config.Project,
		Zone:    zone,
		Type:    OperationWaitZone,
	}
	state := w.Conf()
	state.Delay = 10 * time.Second
	state.Timeout = 10 * time.Minute
	state.MinTimeout = 2 * time.Second
	opRaw, err := state.WaitForState()
	if err != nil {
		return fmt.Errorf("Error waiting for %s: %s", activity, err)
	}
	op = opRaw.(*compute.Operation)
	if op.Error != nil {
		// Return the error
		return OperationError(*op.Error)
	}
	return nil
}


func resourceComputeInstanceCreate(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	// Get the zone
	log.Printf("[DEBUG] Loading zone: %s", d.Get("zone").(string))
	zone, err := config.clientCompute.Zones.Get(
		config.Project, d.Get("zone").(string)).Do()
	if err != nil {
		return fmt.Errorf(
			"Error loading zone '%s': %s", d.Get("zone").(string), err)
	}

	// Get the machine type
	log.Printf("[DEBUG] Loading machine type: %s", d.Get("machine_type").(string))
	machineType, err := config.clientCompute.MachineTypes.Get(
		config.Project, zone.Name, d.Get("machine_type").(string)).Do()
	if err != nil {
		return fmt.Errorf(
			"Error loading machine type: %s",
			err)
	}

	// Build up the list of disks
	disksCount := d.Get("disk.#").(int)
	disks := make([]*compute.AttachedDisk, 0, disksCount)
	for i := 0; i < disksCount; i++ {
		prefix := fmt.Sprintf("disk.%d", i)

		// var sourceLink string

		// Build the disk
		var disk compute.AttachedDisk
		disk.Type = "PERSISTENT"
		disk.Mode = "READ_WRITE"
		disk.Boot = i == 0
		disk.AutoDelete = true

		if v, ok := d.GetOk(prefix + ".auto_delete"); ok {
			disk.AutoDelete = v.(bool)
		}

		// Load up the disk for this disk if specified
		if v, ok := d.GetOk(prefix + ".disk"); ok {
			diskName := v.(string)
			diskData, err := config.clientCompute.Disks.Get(
				config.Project, zone.Name, diskName).Do()
			if err != nil {
				return fmt.Errorf(
					"Error loading disk '%s': %s",
					diskName, err)
			}

			disk.Source = diskData.SelfLink
		}

		// Load up the image for this disk if specified
		if v, ok := d.GetOk(prefix + ".image"); ok {
			imageName := v.(string)


			imageUrl, err := resolveImage(config, imageName)
			if err != nil {
				return fmt.Errorf(
					"Error resolving image name '%s': %s",
					imageName, err)
			}

			disk.InitializeParams = &compute.AttachedDiskInitializeParams{
				SourceImage: imageUrl,
			}
		}

		if v, ok := d.GetOk(prefix + ".type"); ok {
			diskTypeName := v.(string)
			diskType, err := readDiskType(config, zone, diskTypeName)
			if err != nil {
				return fmt.Errorf(
					"Error loading disk type '%s': %s",
					diskTypeName, err)
			}

			disk.InitializeParams.DiskType = diskType.SelfLink
		}

		disks = append(disks, &disk)
	}

	networksCount := d.Get("network.#").(int)
	networkInterfacesCount := d.Get("network_interface.#").(int)

	if networksCount > 0 && networkInterfacesCount > 0 {
		return fmt.Errorf("Error: cannot define both networks and network_interfaces.")
	}
	if networksCount == 0 && networkInterfacesCount == 0 {
		return fmt.Errorf("Error: Must define at least one network_interface.")
	}

	var networkInterfaces []*compute.NetworkInterface

	if networksCount > 0 {
		// TODO: Delete this block when removing network { }
		// Build up the list of networkInterfaces
		networkInterfaces = make([]*compute.NetworkInterface, 0, networksCount)
		for i := 0; i < networksCount; i++ {
			prefix := fmt.Sprintf("network.%d", i)
			// Load up the name of this network
			networkName := d.Get(prefix + ".source").(string)
			network, err := config.clientCompute.Networks.Get(
				config.Project, networkName).Do()
			if err != nil {
				return fmt.Errorf(
					"Error loading network '%s': %s",
					networkName, err)
			}

			// Build the networkInterface
			var iface compute.NetworkInterface
			iface.AccessConfigs = []*compute.AccessConfig{
				&compute.AccessConfig{
					Type:  "ONE_TO_ONE_NAT",
					NatIP: d.Get(prefix + ".address").(string),
				},
			}
			iface.Network = network.SelfLink

			networkInterfaces = append(networkInterfaces, &iface)
		}
	}

	if networkInterfacesCount > 0 {
		// Build up the list of networkInterfaces
		networkInterfaces = make([]*compute.NetworkInterface, 0, networkInterfacesCount)
		for i := 0; i < networkInterfacesCount; i++ {
			prefix := fmt.Sprintf("network_interface.%d", i)
			// Load up the name of this network_interfac
			networkName := d.Get(prefix + ".network").(string)
			network, err := config.clientCompute.Networks.Get(
				config.Project, networkName).Do()
			if err != nil {
				return fmt.Errorf(
					"Error referencing network '%s': %s",
					networkName, err)
			}

			// Build the networkInterface
			var iface compute.NetworkInterface
			iface.Network = network.SelfLink

			// Handle access_config structs
			accessConfigsCount := d.Get(prefix + ".access_config.#").(int)
			iface.AccessConfigs = make([]*compute.AccessConfig, accessConfigsCount)
			for j := 0; j < accessConfigsCount; j++ {
				acPrefix := fmt.Sprintf("%s.access_config.%d", prefix, j)
				iface.AccessConfigs[j] = &compute.AccessConfig{
					Type:  "ONE_TO_ONE_NAT",
					NatIP: d.Get(acPrefix + ".nat_ip").(string),
				}
			}

			networkInterfaces = append(networkInterfaces, &iface)
		}
	}

	serviceAccountsCount := d.Get("service_account.#").(int)
	serviceAccounts := make([]*compute.ServiceAccount, 0, serviceAccountsCount)
	for i := 0; i < serviceAccountsCount; i++ {
		prefix := fmt.Sprintf("service_account.%d", i)

		scopesCount := d.Get(prefix + ".scopes.#").(int)
		scopes := make([]string, 0, scopesCount)
		for j := 0; j < scopesCount; j++ {
			scope := d.Get(fmt.Sprintf(prefix+".scopes.%d", j)).(string)
			scopes = append(scopes, canonicalizeServiceScope(scope))
		}

		serviceAccount := &compute.ServiceAccount{
			Email:  "default",
			Scopes: scopes,
		}

		serviceAccounts = append(serviceAccounts, serviceAccount)
	}

	// Create the instance information
	instance := compute.Instance{
		CanIpForward:      d.Get("can_ip_forward").(bool),
		Description:       d.Get("description").(string),
		Disks:             disks,
		MachineType:       machineType.SelfLink,
		Metadata:          resourceInstanceMetadata(d),
		Name:              d.Get("name").(string),
		NetworkInterfaces: networkInterfaces,
		Tags:              resourceInstanceTags(d),
		ServiceAccounts:   serviceAccounts,
	}

	log.Printf("[INFO] Requesting instance creation")
	op, err := config.clientCompute.Instances.Insert(
		config.Project, zone.Name, &instance).Do()
	if err != nil {
		return fmt.Errorf("Error creating instance: %s", err)
	}

	// Store the ID now
	d.SetId(instance.Name)

	// Wait for the operation to complete
	waitErr := resourceOperationWaitZone(config, op, zone.Name, "instance to create")
	if waitErr != nil {
		// The resource didn't actually create
		d.SetId("")
		return waitErr
	}

	return resourceComputeInstanceRead(d, meta)
}

func resourceComputeInstanceRead(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	instance, err := config.clientCompute.Instances.Get(
		config.Project, d.Get("zone").(string), d.Id()).Do()
	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 404 {
			// The resource doesn't exist anymore
			d.SetId("")

			return nil
		}

		return fmt.Errorf("Error reading instance: %s", err)
	}

	d.Set("can_ip_forward", instance.CanIpForward)

	// Set the service accounts
	for i, serviceAccount := range instance.ServiceAccounts {
		prefix := fmt.Sprintf("service_account.%d", i)
		d.Set(prefix+".email", serviceAccount.Email)
		d.Set(prefix+".scopes.#", len(serviceAccount.Scopes))
		for j, scope := range serviceAccount.Scopes {
			d.Set(fmt.Sprintf("%s.scopes.%d", prefix, j), scope)
		}
	}

	networksCount := d.Get("network.#").(int)
	networkInterfacesCount := d.Get("network_interface.#").(int)

	if networksCount > 0 && networkInterfacesCount > 0 {
		return fmt.Errorf("Error: cannot define both networks and network_interfaces.")
	}
	if networksCount == 0 && networkInterfacesCount == 0 {
		return fmt.Errorf("Error: Must define at least one network_interface.")
	}

	// Set the networks
	// Use the first external IP found for the default connection info.
	externalIP := ""
	internalIP := ""
	if networksCount > 0 {
		// TODO: Remove this when realizing deprecation of .network
		for i, iface := range instance.NetworkInterfaces {
			prefix := fmt.Sprintf("network.%d", i)
			d.Set(prefix+".name", iface.Name)
			log.Printf(prefix+".name = %s", iface.Name)

			var natIP string
			for _, config := range iface.AccessConfigs {
				if config.Type == "ONE_TO_ONE_NAT" {
					natIP = config.NatIP
					break
				}
			}

			if externalIP == "" && natIP != "" {
				externalIP = natIP
			}
			d.Set(prefix+".external_address", natIP)

			d.Set(prefix+".internal_address", iface.NetworkIP)
		}
	}

	if networkInterfacesCount > 0 {
		for i, iface := range instance.NetworkInterfaces {

			prefix := fmt.Sprintf("network_interface.%d", i)
			d.Set(prefix+".name", iface.Name)

			// The first non-empty ip is left in natIP
			var natIP string
			for j, config := range iface.AccessConfigs {
				acPrefix := fmt.Sprintf("%s.access_config.%d", prefix, j)
				d.Set(acPrefix+".nat_ip", config.NatIP)
				if natIP == "" {
					natIP = config.NatIP
				}
			}

			if externalIP == "" {
				externalIP = natIP
			}

			d.Set(prefix+".address", iface.NetworkIP)
			if internalIP == "" {
				internalIP = iface.NetworkIP
			}


		}
	}

	// Fall back on internal ip if there is no external ip.  This makes sense in the situation where
	// terraform is being used on a cloud instance and can therefore access the instances it creates
	// via their internal ips.
	sshIP := externalIP
	if sshIP == "" {
		sshIP = internalIP
	}

	// Initialize the connection info
	d.SetConnInfo(map[string]string{
		"type": "ssh",
		"host": sshIP,
	})

	// Set the metadata fingerprint if there is one.
	if instance.Metadata != nil {
		d.Set("metadata_fingerprint", instance.Metadata.Fingerprint)
	}

	// Set the tags fingerprint if there is one.
	if instance.Tags != nil {
		d.Set("tags_fingerprint", instance.Tags.Fingerprint)
	}

	d.Set("self_link", instance.SelfLink)

	return nil
}

func resourceComputeInstanceUpdate(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	zone := d.Get("zone").(string)

	instance, err := config.clientCompute.Instances.Get(
		config.Project, zone, d.Id()).Do()
	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 404 {
			// The resource doesn't exist anymore
			d.SetId("")

			return nil
		}

		return fmt.Errorf("Error reading instance: %s", err)
	}

	// Enable partial mode for the resource since it is possible
	d.Partial(true)

	// If the Metadata has changed, then update that.
	if d.HasChange("metadata") {
		metadata := resourceInstanceMetadata(d)
		op, err := config.clientCompute.Instances.SetMetadata(
			config.Project, zone, d.Id(), metadata).Do()
		if err != nil {
			return fmt.Errorf("Error updating metadata: %s", err)
		}

		// 1 5 2
		opErr := resourceOperationWaitZone(config, op, zone, "metadata to update")
		if opErr != nil {
			return opErr
		}

		d.SetPartial("metadata")
	}

	if d.HasChange("tags") {
		tags := resourceInstanceTags(d)
		op, err := config.clientCompute.Instances.SetTags(
			config.Project, zone, d.Id(), tags).Do()
		if err != nil {
			return fmt.Errorf("Error updating tags: %s", err)
		}

		opErr := resourceOperationWaitZone(config, op, zone, "tags to update")
		if opErr != nil {
			return opErr
		}

		d.SetPartial("tags")
	}

	networkInterfacesCount := d.Get("network_interface.#").(int)
	if networkInterfacesCount > 0 {
		// Sanity check
		if networkInterfacesCount != len(instance.NetworkInterfaces) {
			return fmt.Errorf("Instance had unexpected number of network interfaces: %d", len(instance.NetworkInterfaces))
		}
		for i := 0; i < networkInterfacesCount; i++ {
			prefix := fmt.Sprintf("network_interface.%d", i)
			instNetworkInterface := instance.NetworkInterfaces[i]
			networkName := d.Get(prefix+".name").(string)

			// TODO: This sanity check is broken by #929, disabled for now (by forcing the equality)
			networkName = instNetworkInterface.Name
			// Sanity check
			if networkName != instNetworkInterface.Name {
				return fmt.Errorf("Instance networkInterface had unexpected name: %s", instNetworkInterface.Name)
			}

			if d.HasChange(prefix+".access_config") {

				// TODO: This code deletes then recreates accessConfigs.  This is bad because it may
				// leave the machine inaccessible from either ip if the creation part fails (network
				// timeout etc).  However right now there is a GCE limit of 1 accessConfig so it is
				// the only way to do it.  In future this should be revised to only change what is
				// necessary, and also add before removing.

				// Delete any accessConfig that currently exists in instNetworkInterface
				for _, ac := range(instNetworkInterface.AccessConfigs) {
					op, err := config.clientCompute.Instances.DeleteAccessConfig(
						config.Project, zone, d.Id(), networkName, ac.Name).Do();
					if err != nil {
						return fmt.Errorf("Error deleting old access_config: %s", err)
					}
					opErr := resourceOperationWaitZone(config, op, zone, "old access_config to delete")
					if opErr != nil {
						return opErr
					}
				}

				// Create new ones
				accessConfigsCount := d.Get(prefix + ".access_config.#").(int)
				for j := 0; j < accessConfigsCount; j++ {
					acPrefix := fmt.Sprintf("%s.access_config.%d", prefix, j)
					ac := &compute.AccessConfig{
						Type:  "ONE_TO_ONE_NAT",
						NatIP: d.Get(acPrefix + ".nat_ip").(string),
					}
					op, err := config.clientCompute.Instances.AddAccessConfig(
						config.Project, zone, d.Id(), networkName, ac).Do();
					if err != nil {
						return fmt.Errorf("Error adding new access_config: %s", err)
					}
					opErr := resourceOperationWaitZone(config, op, zone, "new access_config to add")
					if opErr != nil {
						return opErr
					}
				}
			}
		}
	}

	// We made it, disable partial mode
	d.Partial(false)

	return resourceComputeInstanceRead(d, meta)
}

func resourceComputeInstanceDelete(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	zone := d.Get("zone").(string)
	op, err := config.clientCompute.Instances.Delete(config.Project, zone, d.Id()).Do()
	if err != nil {
		return fmt.Errorf("Error deleting instance: %s", err)
	}

	// Wait for the operation to complete
	opErr := resourceOperationWaitZone(config, op, zone, "instance to delete")
	if opErr != nil {
		return opErr
	}

	d.SetId("")
	return nil
}

func resourceInstanceMetadata(d *schema.ResourceData) *compute.Metadata {
	var metadata *compute.Metadata
	if metadataList := d.Get("metadata").([]interface{}); len(metadataList) > 0 {
		m := new(compute.Metadata)
		m.Items = make([]*compute.MetadataItems, 0, len(metadataList))
		for _, metadataMap := range metadataList {
			for key, val := range metadataMap.(map[string]interface{}) {
				// TODO: fix https://github.com/hashicorp/terraform/issues/883
				//       and remove this workaround <3 phinze
				if key == "#" {
					continue
				}
				m.Items = append(m.Items, &compute.MetadataItems{
					Key:   key,
					Value: val.(string),
				})
			}
		}

		// Set the fingerprint. If the metadata has never been set before
		// then this will just be blank.
		m.Fingerprint = d.Get("metadata_fingerprint").(string)

		metadata = m
	}

	return metadata
}

func resourceInstanceTags(d *schema.ResourceData) *compute.Tags {
	// Calculate the tags
	var tags *compute.Tags
	if v := d.Get("tags"); v != nil {
		vs := v.(*schema.Set)
		tags = new(compute.Tags)
		tags.Items = make([]string, vs.Len())
		for i, v := range vs.List() {
			tags.Items[i] = v.(string)
		}

		tags.Fingerprint = d.Get("tags_fingerprint").(string)
	}

	return tags
}
