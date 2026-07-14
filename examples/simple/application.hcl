poller {
  enabled    = true
  interval   = "10s"
  batch_size = 25
}

http {
  address      = "127.0.0.1:9090"
  read_timeout = "15s"
}

prefix = "/v1"

route "health" {
  target  = "/healthz"
  methods = ["GET", "HEAD"]
}

route "metrics" {
  target  = "/metrics"
  methods = ["GET"]
}