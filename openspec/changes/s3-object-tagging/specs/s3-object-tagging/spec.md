# S3 Object Tagging

## Purpose

Let clients attach, read and remove the tag set S3 semantics attach to an object.

## Requirements

### Requirement: Tag Subresource

`GET`, `PUT` and `DELETE` on `?tagging` SHALL operate on the object's tag set.

#### Scenario: Put then get

- **WHEN** a client stores a tag set and reads it back
- **THEN** the same keys and values are returned in a `<Tagging><TagSet>` document

#### Scenario: No tags

- **WHEN** an object has never been tagged
- **THEN** `GET ?tagging` answers `200` with an empty `<TagSet>`

#### Scenario: Delete

- **WHEN** a client deletes the tag set
- **THEN** the response is `204` and a later `GET` returns an empty `<TagSet>`

#### Scenario: Unknown object

- **WHEN** the object does not exist
- **THEN** the response is `404 NoSuchKey`, for read, write and delete alike

### Requirement: Tag Rules

The tag set SHALL be validated before it is stored.

#### Scenario: Too many tags

- **WHEN** a client sends 11 tags
- **THEN** the response is `400 InvalidTag`

#### Scenario: Invalid key or value

- **WHEN** a key is empty or longer than 128 characters, a value is longer than 256
  characters, a key repeats, or either contains a control character
- **THEN** the response is `400 InvalidTag`

#### Scenario: Malformed document

- **WHEN** the body is not a well-formed Tagging document
- **THEN** the response is `400 MalformedXML`

### Requirement: Header Form

`x-amz-tagging` on a plain PUT SHALL be stored as the object's tag set.

#### Scenario: Tagging at upload

- **WHEN** a client uploads with `x-amz-tagging: team=storage`
- **THEN** `GET ?tagging` reports that tag

### Requirement: Storage Shape

Tags SHALL be persisted in the S3 metadata sidecar in the S3 wire form.

#### Scenario: Replication

- **WHEN** an object with tags is replicated
- **THEN** the tags travel with the object's other S3 metadata

#### Scenario: Deterministic encoding

- **WHEN** a tag set is stored
- **THEN** its encoded form is key-sorted and reversible, so two clients sending
  the same tags produce the same stored value
