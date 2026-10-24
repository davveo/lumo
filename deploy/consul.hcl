# Persistent single-server registry for the local Docker environment.
server = true
bootstrap_expect = 1
node_name = "lumo-consul"
datacenter = "lumo-local"
data_dir = "/consul/data"
bind_addr = "{{ GetInterfaceIP \"eth0\" }}"
client_addr = "0.0.0.0"
ui_config {
  enabled = true
}
disable_update_check = true
leave_on_terminate = true
log_level = "WARN"
enable_script_checks = false
enable_local_script_checks = false
