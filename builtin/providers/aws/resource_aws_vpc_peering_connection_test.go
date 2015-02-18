package aws

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
	"github.com/mitchellh/goamz/ec2"
)

func TestAccAWSVPCPeeringConnection_normal(t *testing.T) {
	var conf ec2.Address

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSVpcPeeringConnectionDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccVpcPeeringConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSVpcPeeringConnectionExists("aws_vpc_peering_connection.foo", &conf),
				),
			},
		},
	})
}

func testAccCheckAWSVpcPeeringConnectionDestroy(s *terraform.State) error {
	conn := testAccProvider.Meta().(*AWSClient).ec2conn

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "aws_vpc_peering_connection" {
			continue
		}

		describe, err := conn.DescribeVpcPeeringConnection([]string{rs.Primary.ID}, ec2.NewFilter())

		if err == nil {
			if len(describe.VpcPeeringConnections) != 0 {
				return fmt.Errorf("vpc peering connection still exists")
			}
		}
	}

	return nil
}

func testAccCheckAWSVpcPeeringConnectionExists(n string, res *ec2.Address) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No vpc peering connection id is set")
		}

		return nil
	}
}

const testAccVpcPeeringConfig = `
resource "aws_vpc" "foo" {
    cidr_block = "10.0.0.0/16"
}

resource "aws_vpc" "bar" {
    cidr_block = "10.0.1.0/16"
}

resource "aws_vpc_peering_connection" "foo" {
    peer_owner_id = "12345"
    vpc_id = "${aws_vpc.foo.id}"
    peer_vpc_id = "${aws_vpc.bar.id}"
}
`
