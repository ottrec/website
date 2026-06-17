---
title: Ottawa Neighborhoods & Suburbs
date: June 16, 2026
author: Patrick Gaskin
description: How ottrec.ca divides the Ottawa area into areas like Kanata/Westboro/Orleans, and central/south/east/west.
class: regions-page

about_label: regions & sectors
about_blurb: how facilities are grouped into areas like Kanata, Westboro, and Orléans.

home_label: ottregions
home_blurb: how the region/sector labels for facilities are determined
---

<!--
    Copyright (c) 2026 Patrick Gaskin.
    This content is not under LICENSE, and may not be used anywhere other than ottrec.ca.
-->

To make it easier to figure out the general location of unknown facilities (the address is kind of unhelpful), I group facilities into central/east/west/south sectors and include a label for the general region.

This page explains my methodology to determine the grouping.

```block regions-map
```

## Regions

Defining the boundaries of areas such as Gloucester, Kanata, Orléans, Vanier, Centretown and Westboro is more challenging than it might initially appear. While any person in Ottawa could probably tell you where they are, there isn't any authoritative source clearly defining the boundaries, especially if you don't want the names to be general enough to be useful for someone not already familiar with the area.

If you break things down by the official [ward boundaries](https://ottawa.ca/en/city-hall/elections/ward-maps-and-school-board-zones/ward-maps), it mostly works. However, what most people would refer to as "Westboro" is actually named "Kitchissippi", "Alta Vista" is spread across multiple, and downtown areas are split across several wards, making them a poor match for intuitive labels.

If you instead use detailed neighbourhood definitions, the [Ottawa Neighbourhood Study](https://ons-sqo.ca/) provides a well-documented set of boundaries with associated demographic data. However, these neighbourhoods are intentionally granular and statistically oriented, resulting in a level of fragmentation that is too fine for it to be used as a high-level grouping mechanism.

Postal code areas provide another alternative, but they are designed for mail routing rather than geography. As a result, they often produce awkward splits in suburban regions such as Orléans and Barrhaven, and they can vary significantly in spatial size, making them too inconsistent for this purpose.

I eventually realized that the labels already present on common mapping tools (e.g., [Google Maps](https://maps.google.com), [OpenStreetMap](https://www.openstreetmap.org)) were basically what I wanted. However, further investigation revealed that these labels correspond to a point near the centre rather than defined polygons I could use to group facilities.

A common way to derive regions from such point data is to construct a Voronoi diagram, which partitions space so that every location is assigned to the nearest labelled point. This produces a set of non-overlapping cells that approximate areas around each place name.

I implemented this approach using OpenStreetMap data (via the [Overpass API](https://wiki.openstreetmap.org/wiki/Overpass_API)), and it turns out that it classifies almost all facilities the way you'd expect, while remaining deterministic, reproducible, and free from subjective boundary decisions.

## Sectors

For the more broad central/south/east/west classifications, I just drew some approximate lines.

## Region centrepoints

This is the OpenStreetMap data I use to determine the boundaries.

```block regions-table
```
