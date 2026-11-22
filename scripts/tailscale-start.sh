#!/system/bin/sh
export HOME=/data/tailscale

mkdir -p /data/tailscale/state
mkdir -p /data/tailscale/.cache
mkdir -p /dev/net
ln -sf /dev/tun /dev/net/tun

# Ensure tailscaled's own fwmark (0x80000) resolves via Android's wlan0 table
# before tailscaled's priority 5250 unreachable rule.
# After a cold boot, wait up to 60s for netd to register the wlan0 routing table.
ip rule del pref 5240 2>/dev/null || true
i=0
while [ $i -lt 30 ]; do
    if ip rule add pref 5240 fwmark 0x80000/0xff0000 lookup wlan0 2>/dev/null; then
        break
    fi
    if ip rule show | grep -q "^5240:"; then
        break
    fi
    i=$((i + 1))
    sleep 2
done

ip -6 rule del pref 5240 2>/dev/null || true
ip -6 rule add pref 5240 fwmark 0x80000/0xff0000 lookup wlan0 2>/dev/null || true

# If already running and responsive, exit cleanly
if [ -S /data/tailscale/tailscaled.sock ]; then
    if /data/tailscale/bin/tailscale --socket=/data/tailscale/tailscaled.sock status >/dev/null 2>&1; then
        exit 0
    fi
fi

# Clean up any stale process/socket
killall tailscaled 2>/dev/null || true
rm -f /data/tailscale/tailscaled.sock

# Run tailscaled detached in its own session so it survives shell exit
/system/bin/setsid /data/tailscale/bin/tailscaled \
    --statedir=/data/tailscale/state \
    --socket=/data/tailscale/tailscaled.sock \
    --port=41641 \
    </dev/null >/data/tailscale/tailscaled.log 2>&1 &
