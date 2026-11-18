# Multipart Upload Footprint

## Purpose

Make the disk an in-flight multipart upload holds observable, so capacity planning
can separate upload churn from object growth.

## Requirements

### Requirement: Report Open Uploads And Their Spill

The metrics endpoint SHALL report the number of open multipart uploads and the
bytes their spooled parts hold.

#### Scenario: Idle node

- **WHEN** no multipart upload is open
- **THEN** both families are present with the value `0`

#### Scenario: In-flight upload

- **WHEN** one upload holds a single 2 MiB part
- **THEN** `momo_multipart_uploads_active` is `1` and
  `momo_multipart_spill_bytes` is `2097152`

#### Scenario: Several parts

- **WHEN** an upload holds parts of 100 and 250 bytes
- **THEN** the byte gauge counts both (`350`)

### Requirement: Scrape Is Cheap

#### Scenario: No directory walk

- **WHEN** a scrape runs while uploads are open
- **THEN** the values are derived from the session map, not from walking the spill
  directory
