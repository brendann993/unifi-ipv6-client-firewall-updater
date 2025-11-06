# UniFi IPv6 Updater

This Go application monitors on a schedule for IPv6 address changes of a client/device connected to a UniFi controller and updates a firewall address group/list if it changes.

## Environment Variables

The following environment variables are required:

- `UNIFI_HOST`: the URL of the UniFi controller
- `UNIFI_API_KEY`: the API key for the UniFi controller

Optional environment variables:

- `CONFIG_PATH`: the path to the configuration file (default: `/app/clients.json`)
- `CHECK_INTERVAL`: the interval in seconds to check for IPv6 address changes (default: 3600 = 1 hour)
- `VERIFY_SSL`: whether to verify SSL certificates when connecting to the UniFi controller (default: true)

## Configuration File

The configuration file is expected to be in JSON format. It should contain the following information:

- `clients`: an array of client information, including
  - `mac`: the MAC address of the client
  - `group_id`: the ID of the firewall address group to update
  - `last_ipv6`: the last known IPv6 address of the client

Example configuration file:
```
{
  "clients": [
    {
      "mac": "98:b0:37:cd:5a:e4",
      "group_id": "8832fdke0c522972oe9f6200",
      "last_ipv6": ""
    }
  ]
}
```