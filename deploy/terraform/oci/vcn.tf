resource "oci_core_vcn" "ec_vcn" {
  compartment_id = oci_identity_compartment.ec_stack.id
  cidr_blocks    = [var.vcn_cidr]
  display_name   = "ec-stack-vcn"
  dns_label      = "ecvcn"
  freeform_tags  = var.tags
}

resource "oci_core_internet_gateway" "ec_igw" {
  compartment_id = oci_identity_compartment.ec_stack.id
  vcn_id         = oci_core_vcn.ec_vcn.id
  display_name   = "ec-internet-gw"
  enabled        = true
  freeform_tags  = var.tags
}

resource "oci_core_route_table" "ec_public_rt" {
  compartment_id = oci_identity_compartment.ec_stack.id
  vcn_id         = oci_core_vcn.ec_vcn.id
  display_name   = "ec-public-rt"
  freeform_tags  = var.tags

  route_rules {
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
    network_entity_id = oci_core_internet_gateway.ec_igw.id
  }
}

resource "oci_core_security_list" "ec_public_sl" {
  compartment_id = oci_identity_compartment.ec_stack.id
  vcn_id         = oci_core_vcn.ec_vcn.id
  display_name   = "ec-public-sl"
  freeform_tags  = var.tags

  egress_security_rules {
    destination = "0.0.0.0/0"
    protocol    = "all"
    stateless   = false
  }

  ingress_security_rules {
    source    = "0.0.0.0/0"
    protocol  = "6" # TCP
    stateless = false
    tcp_options {
      min = 22
      max = 22
    }
  }

  ingress_security_rules {
    source    = var.vcn_cidr
    protocol  = "6"
    stateless = false
    tcp_options {
      min = 8080
      max = 8080
    }
  }

  ingress_security_rules {
    source    = var.vcn_cidr
    protocol  = "6"
    stateless = false
    tcp_options {
      min = 6333
      max = 6334
    }
  }
}

resource "oci_core_subnet" "ec_public" {
  compartment_id    = oci_identity_compartment.ec_stack.id
  vcn_id            = oci_core_vcn.ec_vcn.id
  cidr_block        = var.public_subnet_cidr
  display_name      = "ec-public-subnet"
  dns_label         = "ecpub"
  route_table_id    = oci_core_route_table.ec_public_rt.id
  security_list_ids = [oci_core_security_list.ec_public_sl.id]
  freeform_tags     = var.tags
}

resource "oci_core_nat_gateway" "ec_natgw" {
  compartment_id = oci_identity_compartment.ec_stack.id
  vcn_id         = oci_core_vcn.ec_vcn.id
  display_name   = "ec-nat-gw"
  freeform_tags  = var.tags
}

resource "oci_core_route_table" "ec_private_rt" {
  compartment_id = oci_identity_compartment.ec_stack.id
  vcn_id         = oci_core_vcn.ec_vcn.id
  display_name   = "ec-private-rt"
  freeform_tags  = var.tags

  route_rules {
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
    network_entity_id = oci_core_nat_gateway.ec_natgw.id
  }
}

resource "oci_core_subnet" "ec_private" {
  compartment_id             = oci_identity_compartment.ec_stack.id
  vcn_id                     = oci_core_vcn.ec_vcn.id
  cidr_block                 = var.private_subnet_cidr
  display_name               = "ec-private-subnet"
  dns_label                  = "ecpriv"
  prohibit_public_ip_on_vnic = true
  route_table_id             = oci_core_route_table.ec_private_rt.id
  security_list_ids          = [oci_core_security_list.ec_public_sl.id]
  freeform_tags              = var.tags
}
