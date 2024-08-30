import { LDFlagValue } from 'launchdarkly-js-client-sdk';

const API_BASE = import.meta.env.PROD ? '' : '/api';
export const apiRoute = (pathname: string) => `${API_BASE}${pathname}`;

export const sortFlags = (flags: Record<string, LDFlagValue>) =>
  Object.keys(flags)
    .sort((a, b) => a.localeCompare(b))
    .reduce<Record<string, LDFlagValue>>((accum, flagKey) => {
      accum[flagKey] = flags[flagKey];

      return accum;
    }, {});
