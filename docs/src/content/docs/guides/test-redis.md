---
title: Seed and check Redis
description: Seed keys into Redis or Valkey, Dragonfly, KeyDB and Garnet, and check the keys your services write there, by their value, their JSON properties or hash fields, or that they are gone.
---

Services keep what they are asked for often in Redis: a cached view, an idempotency key, a session. The `redis` pack seeds keys before a scenario acts, and checks what your service wrote, or that it dropped a key it had to drop. It works with every server that speaks Redis's protocol: Redis itself, Valkey, Dragonfly, KeyDB and Garnet.

```gherkin
Scenario: A scan drops the parcel's cached tracking view
  Given a seeds/cache-dropped.yaml db seed
  And a redis/tracking-PX-RDS-9603.yaml redis seed
  When a message is published to the depots/LEJ/scans mqtt topic:
    """
    {"scanId": "SC-9603-1", "parcelRef": "PX-RDS-9603", "status": "OUT_FOR_DELIVERY"}
    """
  Then within 20s the tracking:PX-RDS-9603 redis key does not exist
```

Add the pack to the project with `axx pack add redis` ([Choose packs](/guides/use-packs/)).

## Register the server

Register the server once, usually in the `Background`:

```gherkin
Background:
  Given the cache redis server with the following properties:
    | url | redis://${sys:local.host}:6379/0 |
```

The `url` is `redis://user:password@host:6379/0`, or `rediss://` for TLS. A `database` row picks another database than the URL's. A password, in the URL or from `${env:..}`, is masked in logs and failures.

## Seed keys

A seed file maps each key to what it holds, and optionally how long it lasts:

```yaml title="redis/tracking-PX-RDS-9602.yaml"
"tracking:PX-RDS-9602":
  value: {"parcelRef": "PX-RDS-9602", "status": "OUT_FOR_DELIVERY", "lastLocation": "Leipzig"}
  ttl: 10m
"shop:hawthorn-home":
  hash: {name: Hawthorn Home, tier: gold}
"depot:LEJ:sorting":
  list: [PX-RDS-9601, PX-RDS-9602]
"parcels:express":
  set: [PX-RDS-9611, PX-RDS-9612]
"couriers:deliveries":
  sorted set: {CR-LEJ-12: 42, CR-LEJ-7: 17}
```

```gherkin
Given a redis/tracking-PX-RDS-9602.yaml redis seed
```

- **What a key holds:** `value` is text, or JSON for an object or an array. The other kinds are `hash`, `list`, `set` and `sorted set`, whose members map to their scores.
- **Replacing:** a seed replaces any key of the same name, so a scenario starts from what its seed says.
- **`ttl`:** a duration like `10m`, after which the key expires.

## Check keys

```gherkin
Then the idempotency:rowanberry-crafts:order-8812 redis key has the value 'PX-RDS-9604'
And the tracking:PX-RDS-9601 redis key has the following properties:
  | parcelRef | PX-RDS-9601 |
  | status    | REGISTERED  |
And within 20s the tracking:PX-RDS-9603 redis key does not exist
```

- `has the value` compares a string key's text, all of it.
- `has the following properties:` reads the key as JSON, whatever its type:
  - a string holding JSON is that JSON;
  - a hash is an object of its fields;
  - a list is an array;
  - a set is a sorted array;
  - a sorted set is an object of its members and their scores.

  Each row is a path into it and the value it has, as in the other JSON property steps: `null` for null and `undefined` for absent.
- `does not exist` checks that a key is gone, for a service that deletes it or lets it expire.

Every check waits for the key to be as it says: 10 seconds, or the time `within` gives.

## Keep scenarios apart

Scenarios run in parallel against the same server, so each seeds and checks keys of its own, named after data unique to it, such as a parcel reference. An `axx lint` rule can keep seeded key names unique across files:

```yaml title="axx.yaml"
lint:
  rules:
    - name: Redis keys in seeds
      filePatterns: ["redis/*.yaml"]
      regex: '^"([^"]+)":'
      validation: cross-file-unique
```

See the [redis pack's reference](/references/packs/redis/) for every step.
