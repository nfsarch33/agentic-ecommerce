resource "oci_identity_compartment" "ec_stack" {
  compartment_id = var.tenancy_ocid
  name           = var.compartment_name
  description    = "Compartment for EC stack resources (mem0, Qdrant, supporting infra)."
  freeform_tags  = var.tags

  enable_delete = true
}
