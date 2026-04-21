# ZeroPhone Production Deployment

## Domain Setup

1. Point your domain DNS to server IP:
   - A record: @ → your_server_ip  
   - A record: www → your_server_ip
   - A record: turn → your_server_ip

2. Install and run certbot:
```bash
sudo apt update
sudo apt install nginx certbot python3-certbot-nginx -y
sudo certbot --nginx -d voip.wadiem.dnscloud.be
```

## Files Needed

1. Copy nginx.conf to /etc/nginx/sites-available/zerophone
2. Create static folder: sudo mkdir -p /var/www/zerophone
3. Copy static files: cp -r static/* /var/www/zerophone/

## Commands

```bash
# Test nginx
sudo nginx -t

# Reload
sudo systemctl reload nginx

# Check SSL
curl -k https://voip.wadiem.dnscloud.be/status
```

## WebSocket URL
wss://voip.wadiem.dnscloud.be/ws/{user_id}