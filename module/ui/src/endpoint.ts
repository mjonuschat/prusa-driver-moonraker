// What the add-machine form sends to Prusa Connect, and the small rules that
// turn what a person typed into it. Nothing here touches the network.

/** The values the form collects. Empty strings are values a person left out. */
export interface MachineForm {
  address: string;
  name: string;
  unitUid: string;
  vendor: string;
  model: string;
  /** A Moonraker API key, for a Moonraker that requires authorization. */
  apiKey: string;
}

/** Moonraker's own default port, which the driver also adds to a bare host. */
export const DEFAULT_PORT = '7125';

/** The connector instance the driver's README tells an operator to declare. */
export const DEFAULT_CONNECTOR = 'moonraker';

const MAX_NAME = 63;

/** The address as the driver reads it: trimmed, with no spaces inside. */
export const cleanAddress = (typed: string): string => typed.trim();

/** Whether the text can be an address: a host, host:port, or an http(s)/ws(s) URL. */
export const looksLikeAddress = (typed: string): boolean => {
  const address = cleanAddress(typed);
  if (address === '' || /\s/.test(address)) return false;
  if (address.includes('://')) {
    try {
      const url = new URL(address);

      return (
        ['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) &&
        url.hostname !== ''
      );
    } catch {
      return false;
    }
  }

  return (
    /^[^/:]+(:\d{1,5})?$/.test(address) ||
    /^\[[0-9a-f:]+\](:\d{1,5})?$/i.test(address)
  );
};

/**
 * A name for the endpoint that the API accepts as a path segment, made from
 * the host of the address: `voron.local:7125` becomes `voron-local`.
 */
export const nameFromAddress = (typed: string): string => {
  const address = cleanAddress(typed);
  if (!address.includes('://')) return slug(address.replace(/:\d{1,5}$/, ''));
  try {
    return slug(new URL(address).hostname);
  } catch {
    return slug(address);
  }
};

/** Lower case letters and digits, with single dashes between. */
export const slug = (text: string): string =>
  text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, MAX_NAME)
    .replace(/-+$/g, '');

/** Whether the text is a name the endpoint API will take. */
export const validName = (name: string): boolean =>
  name !== '' && name.length <= MAX_NAME && slug(name) === name;

/** The body of PUT /api/v1/connector-endpoints/{name}. */
export const endpointBody = (
  form: MachineForm,
): {
  connector: string;
  address: string;
  enabled: true;
  options: Record<string, string>;
} => {
  const vendor = form.vendor.trim();
  const model = form.model.trim();
  const apiKey = form.apiKey.trim();
  const options: Record<string, string> = {
    unit_uid: form.unitUid.trim() === '' ? form.name : form.unitUid.trim(),
    ...(vendor === '' ? {} : { vendor }),
    ...(model === '' ? {} : { model }),
    ...(apiKey === '' ? {} : { api_key: apiKey }),
  };

  return {
    connector: DEFAULT_CONNECTOR,
    address: cleanAddress(form.address),
    enabled: true,
    options,
  };
};

/** Create an endpoint, or update the one this wizard already created. */
export const endpointWrite = (
  form: MachineForm,
  created?: { name: string; version: string },
) => {
  const name = created?.name ?? form.name;

  return {
    path: { name },
    header:
      created === undefined
        ? { 'If-None-Match': '*' }
        : { 'If-Match': `"${created.version}"` },
    body: endpointBody({ ...form, name }),
  };
};

/** The endpoint options that name a Klipper object for one role, in the order the edit view lists them. */
export const ROLE_KEYS = [
  'chamber_object',
  'bed_fans_object',
  'part_fan_object',
  'heatbreak_fan_object',
  'filament_object',
  'chamber_light_object',
  'chamber_fan_object',
  'extra_sensors',
  'extra_fans',
] as const;

export type RoleKey = (typeof ROLE_KEYS)[number];

/** What the edit view holds for each role. An empty string is auto-detect. */
export type RoleValues = Record<RoleKey, string>;

/** The role overrides an endpoint's options hold. A role with none reads empty. */
export const rolesOf = (
  options: Record<string, string> | undefined,
): RoleValues =>
  Object.fromEntries(
    ROLE_KEYS.map((key) => [key, options?.[key] ?? '']),
  ) as RoleValues;

/**
 * After an edit, every option the view does not own is kept, and a role left
 * empty is removed so the driver detects it again.
 */
export const withRoles = (
  options: Record<string, string> | undefined,
  values: RoleValues,
): Record<string, string> => {
  const next: Record<string, string> = Object.fromEntries(
    Object.entries(options ?? {}).filter(
      ([key]) => !(ROLE_KEYS as readonly string[]).includes(key),
    ),
  );
  for (const key of ROLE_KEYS) {
    const value = values[key].trim();
    if (value !== '') next[key] = value;
  }

  return next;
};

/** Whether saving these roles would change the endpoint options. */
export const rolesChanged = (
  options: Record<string, string> | undefined,
  values: RoleValues,
): boolean => {
  const previous = options ?? {};
  const next = withRoles(options, values);

  return (
    Object.keys(previous).length !== Object.keys(next).length ||
    Object.entries(next).some(([key, value]) => previous[key] !== value)
  );
};

export const updateBody = (
  endpoint: {
    address: string;
    connector: string;
    enabled: boolean;
  },
  options: Record<string, string>,
): {
  connector: string;
  address: string;
  enabled: boolean;
  options: Record<string, string>;
} => ({
  connector: endpoint.connector,
  address: endpoint.address,
  enabled: endpoint.enabled,
  options,
});
