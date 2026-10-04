#!/usr/bin/env python3
"""Give the stays already recorded the address they were written without.

A stay carries the name of the place and, since 5 October 2026, the
address and how far away the named thing was. The ones written before
that have only the name, and a name on its own cannot be checked: a
cinema at Sky Hub Mall was recorded as "Flavors non veg restaurant"
because that was the nearest thing any map had, 127 metres away across
a trunk road, and nothing in the record said so.

This fills them in. It never touches the name -- everything counts on
that field, and a backfill that renamed places would quietly rewrite
the description of somebody's life.

Places the person fenced themselves are skipped. They drew it, they
know where it is, and asking would spend a credit to be told something
they wrote.

Dry run by default. Pass --write to apply.

    python3 deployments/fill-stay-addresses.py
    python3 deployments/fill-stay-addresses.py --write
"""

import json
import os
import re
import sys
import urllib.parse
import urllib.request

import subprocess

HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CONFIG = os.path.join(HERE, "config.ini")

# The same categories and reach the server uses. Kept in step by hand,
# which is a cost: a backfill that disagrees with the live naming would
# produce rows nothing else would ever have written.
CATEGORIES = ("commercial,catering,entertainment,leisure,education,"
              "healthcare,office,service,public_transport,religion")
REACH = 150.0


def setting(section, key):
    """One value out of config.ini."""
    want, found = False, None
    for line in open(CONFIG):
        line = line.strip()
        if line.startswith("["):
            want = line == "[%s]" % section
            continue
        if want and "=" in line and not line.startswith("#"):
            k, _, v = line.partition("=")
            if k.strip() == key:
                found = v.strip()
    return found or ""


def query(sql):
    """Rows from the database, as lists of strings."""
    out = subprocess.run(
        ["mysql", "-h", setting("database", "host") or "127.0.0.1",
         "-u", setting("database", "user") or "assistant",
         "-p" + setting("database", "password"),
         setting("database", "name") or "assistant", "-N", "-B", "-e", sql],
        capture_output=True, text=True, check=True)
    return [l.split("\t") for l in out.stdout.strip().split("\n") if l]


def geoapify(path, args):
    args["apiKey"] = setting("geoapify", "key")
    url = path + "?" + urllib.parse.urlencode(args)
    with urllib.request.urlopen(url, timeout=20) as r:
        return json.load(r)


def rest(formatted, name):
    """The address with its leading name taken off."""
    formatted, name = (formatted or "").strip(), (name or "").strip()
    if name and formatted.startswith(name + ","):
        return formatted[len(name) + 1:].strip()
    return formatted


def look(lat, lon):
    """What is there: the nearest business, and the address either way."""
    near_name, near_metres = "", 0.0
    places = geoapify("https://api.geoapify.com/v2/places", {
        "categories": CATEGORIES,
        "filter": "circle:%.6f,%.6f,%d" % (lon, lat, int(REACH)),
        "bias": "proximity:%.6f,%.6f" % (lon, lat), "limit": "20"})
    best = REACH
    for f in places.get("features", []):
        p = f.get("properties", {})
        n, d = (p.get("name") or "").strip(), p.get("distance", REACH)
        if n and d <= best:
            near_name, near_metres, best = n, d, d

    reverse = geoapify("https://api.geoapify.com/v1/geocode/reverse",
                       {"lat": "%.6f" % lat, "lon": "%.6f" % lon, "format": "geojson"})
    fs = reverse.get("features") or []
    if not fs:
        return near_name, "", near_metres
    p = fs[0].get("properties", {})
    address = rest(p.get("formatted"), p.get("name"))
    if near_name:
        return near_name, address, near_metres
    return "", address, p.get("distance", 0.0)


def main():
    write = "--write" in sys.argv

    fenced = {r[0] for r in query(
        "select distinct payload->>'$.value' from events "
        "where kind in ('place.entered','place.exited')")}
    print("fenced, so skipped:", ", ".join(sorted(fenced)) or "(none)")

    rows = query(
        "select id, payload->>'$.value', payload->>'$.at' from events "
        "where kind='place.stayed' and payload->>'$.address' is null "
        "and payload->>'$.at' is not null")

    # One lookup a place, not a row. Two hundred and seventy stays at
    # home are one coordinate, and a credit each would be absurd.
    spots, skipped = {}, 0
    for rid, name, at in rows:
        if name in fenced:
            skipped += 1
            continue
        m = re.match(r"\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*$", at or "")
        if not m:
            continue
        key = (round(float(m.group(1)), 4), round(float(m.group(2)), 4))
        spots.setdefault(key, {"name": name, "ids": []})["ids"].append(rid)

    print("rows needing an address: %d (%d skipped as fenced)" % (
        len(rows) - skipped, skipped))
    print("distinct places to look up: %d, at two credits each\n" % len(spots))

    for (lat, lon), spot in spots.items():
        name, address, metres = look(lat, lon)
        print("  %-30s %s" % (spot["name"], address or "(no address)"))
        print("  %-30s nearest named thing: %s at %dm, %d row(s)\n" % (
            "", name or "(none)", round(metres), len(spot["ids"])))
        if not write or not address:
            continue
        ids = ",".join("'%s'" % i for i in spot["ids"])
        query("update events set payload = json_set(payload, "
              "'$.address', %s, '$.metres', %d) where id in (%s)" % (
                  json.dumps(address), round(metres), ids))

    print("written." if write else "dry run -- pass --write to apply.")


if __name__ == "__main__":
    main()
