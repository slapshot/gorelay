# ADR-0003: Operator-allowlisted symbolic destinations

Status: Proposed
Date: 2026-11-05

## Context
An arbitrary producer-supplied URL turns a self-hosted webhook service into an SSRF primitive, including access to host metadata and internal networks.

## Decision
Accept only destination_id, resolved through operator-managed static configuration with per-client authorization, HTTPS policy, DNS/IP validation at dial time, no automatic redirect following, and explicit exceptions for private IP destinations.

## Alternatives
Accept raw URLs with validation: too easy to misconfigure and hard to defend against DNS rebinding. Dynamic destination registry: possible future feature with authenticated admin protocol and audit trail.

## Consequences
Destination provisioning is initially operational rather than a public CRUD API. Security checks must run at connection time and be covered by adversarial tests.

## Verification / reversal
Review policy when a secured destination-management API becomes a real user requirement. Never bypass allowlist to simplify demos.
