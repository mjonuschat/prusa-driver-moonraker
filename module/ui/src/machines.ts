// What the machines table reads from the endpoint listing, and the match from
// an endpoint to the printer it produced. Nothing here touches the network.

import type { Device } from '@prusa/connect-ui/domain';

import { DEFAULT_CONNECTOR } from './endpoint';

/** One row of GET /api/v1/connector-endpoints, as far as this module reads it. */
export interface Endpoint {
  name: string;
  address: string;
  connector: string;
  enabled: boolean;
  options?: Record<string, string> | undefined;
  version: string;
}

/**
 * The endpoints this module manages. The listing holds every connector's
 * rows, and the name a connector is declared under is the operator's, so the
 * one name the wizard defaults to is what counts as a Moonraker machine here.
 */
export const moonrakerEndpoints = (
  items: readonly Endpoint[] | null,
): Endpoint[] =>
  (items ?? [])
    .filter((endpoint) => endpoint.connector === DEFAULT_CONNECTOR)
    .sort((a, b) => a.name.localeCompare(b.name));

/**
 * The identity the driver gives the printer. It is unit_uid, or moonraker: and
 * the address when that is empty, as the driver derives it.
 */
export const identityOf = (endpoint: Endpoint): string => {
  const unit = endpoint.options?.unit_uid;

  return unit === undefined || unit === ''
    ? `moonraker:${endpoint.address}`
    : unit;
};

export const deviceOf = (
  endpoint: Endpoint,
  devices: readonly Device[],
): Device | undefined =>
  devices.find((device) => device.unit_uid === identityOf(endpoint));

/** Where the driver publishes the options that name an object the machine does not list. */
export const UNRESOLVED_PATH = '/moonraker/unresolved';

export interface Unresolved {
  option: string;
  value: string;
}

/**
 * What the driver reported as unresolved for a printer. A tree that holds no
 * list, or entries of another shape, reads as nothing missing.
 */
export const unresolvedOf = (device: Device | undefined): Unresolved[] => {
  const raw = device?.state[UNRESOLVED_PATH];
  if (!Array.isArray(raw)) return [];

  return raw.flatMap((entry: unknown) => {
    if (typeof entry !== 'object' || entry === null) return [];
    const { option, value } = entry as Record<string, unknown>;

    return typeof option === 'string' && typeof value === 'string'
      ? [{ option, value }]
      : [];
  });
};

/** Where the driver publishes the object it settled on for each role. */
export const DETECTED_PATH = '/moonraker/detected';

/**
 * The object detected for each role, by the option that names it. A printer
 * that has published nothing reads undefined, which is not the same as a
 * printer that detected nothing.
 */
export const detectedOf = (
  device: Device | undefined,
): Record<string, string> | undefined => {
  const raw = device?.state[DETECTED_PATH];
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
    return undefined;
  }

  return Object.fromEntries(
    Object.entries(raw).filter(
      (entry): entry is [string, string] => typeof entry[1] === 'string',
    ),
  );
};
