# Qdrant runs alongside mem0 on the same OCI compute instance.
# This file provisions Qdrant-specific block storage and firewall
# rules. The actual Qdrant deployment uses Docker Compose on the
# instance (see deploy/mem0/docker-compose.mem0.yml).

resource "oci_core_volume" "qdrant_data" {
  compartment_id      = oci_identity_compartment.ec_stack.id
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
  display_name        = "ec-qdrant-data"
  size_in_gbs         = var.qdrant_volume_size_gb
  freeform_tags       = var.tags
}

resource "oci_core_volume_attachment" "qdrant_data" {
  attachment_type = "paravirtualized"
  instance_id     = oci_core_instance.mem0.id
  volume_id       = oci_core_volume.qdrant_data.id
  display_name    = "ec-qdrant-data-attachment"
}
