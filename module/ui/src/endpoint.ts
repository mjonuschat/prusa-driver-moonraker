// What the add-machine form sends to Prusa Connect, and the small rules that
// turn what a person typed into it. Nothing here touches the network.

/** The values the form collects. Empty strings are values a person left out. */
export interface MachineForm {
  address: string;
  name: string;
  unitUid: string;
  vendor: string;
  model: string;
  connector: string;
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
  const options: Record<string, string> = {
    unit_uid: form.unitUid.trim() === '' ? form.name : form.unitUid.trim(),
    ...(vendor === '' ? {} : { vendor }),
    ...(model === '' ? {} : { model }),
  };

  return {
    connector:
      form.connector.trim() === '' ? DEFAULT_CONNECTOR : form.connector.trim(),
    address: cleanAddress(form.address),
    enabled: true,
    options,
  };
};
