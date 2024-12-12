# load-guardian
A defensive load balancer in Go that includes DDoS protection features.

This load balancer implements several defensive features:

1. DDoS Protection:
   - Rate limiting per IP address
   - Burst protection
   - Automatic IP blocking for repeat offenders
   - Configurable thresholds and block durations

2. High Availability:
   - Health checking of backend servers
   - Automatic failover
   - Backend recovery detection

3. Load Distribution:
   - Round-robin load balancing
   - Dead backend detection
   - Dynamic backend pool management

To use this load balancer:

1. Create a config.json file:
```json
{
    "backends": [
        "http://backend1:8081",
        "http://backend2:8082",
        "http://backend3:8083"
    ],
    "rate_limit": 60,
    "burst_limit": 10,
    "block_threshold": 3,
    "block_duration": 30
}
```

2. Run the load balancer:
```bash
go run loadbalancer.go
```
Remember to always combine this with other security measures like:
- Web Application Firewall (WAF)
- SSL/TLS encryption
- Regular security audits
- Network monitoring
