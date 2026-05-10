data "oci_identity_availability_domains" "ads" {
  compartment_id = var.tenancy_ocid
}

resource "oci_core_instance" "mem0" {
  compartment_id      = oci_identity_compartment.ec_stack.id
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
  display_name        = "ec-mem0"
  shape               = var.mem0_shape
  freeform_tags       = var.tags

  shape_config {
    ocpus         = var.mem0_ocpus
    memory_in_gbs = var.mem0_memory_gb
  }

  source_details {
    source_type = "image"
    source_id   = var.mem0_image_ocid
  }

  create_vnic_details {
    subnet_id        = oci_core_subnet.ec_public.id
    assign_public_ip = true
    display_name     = "ec-mem0-vnic"
    freeform_tags    = var.tags
  }

  metadata = {
    ssh_authorized_keys = var.ssh_public_key
    user_data = base64encode(templatefile("${path.module}/cloud-init.tftpl", {
      mem0_port   = 8080
      qdrant_port = 6333
    }))
  }
}
