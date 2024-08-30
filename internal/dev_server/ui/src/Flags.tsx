import { LDFlagSet, LDFlagValue } from 'launchdarkly-js-client-sdk';
import { Button, Checkbox, IconButton, Label } from '@launchpad-ui/components';
import { Box, CopyToClipboard } from '@launchpad-ui/core';
import Theme from '@launchpad-ui/tokens';
import { useEffect, useState } from 'react';
import { Icon } from '@launchpad-ui/icons';
import { apiRoute, sortFlags } from './util.ts';
import { FlagsApiResponse, FlagVariation } from './api.ts';
import VariationValues from './Flag.tsx';

type FlagProps = {
  selectedProject: string;
  flags: LDFlagSet | null;
  setFlags: (flags: LDFlagSet) => void;
};

function Flags({ selectedProject, flags, setFlags }: FlagProps) {
  const [overrides, setOverrides] = useState<
    Record<string, { value: LDFlagValue }>
  >({});
  const [onlyShowOverrides, setOnlyShowOverrides] = useState(false);
  const [availableVariations, setAvailableVariations] = useState<
    Record<string, FlagVariation[]>
  >({});

  const overridesPresent = overrides && Object.keys(overrides).length > 0;

  const updateOverride = (flagKey: string, overrideValue: LDFlagValue) => {
    fetch(apiRoute(`/dev/projects/${selectedProject}/overrides/${flagKey}`), {
      method: 'PUT',
      body: JSON.stringify(overrideValue),
    })
      .then(async (res) => {
        if (!res.ok) {
          throw new Error(
            `got ${res.status} ${res.statusText}. ${await res.text()}`,
          );
        }

        const updatedOverrides = {
          ...overrides,
          ...{
            [flagKey]: {
              value: overrideValue,
            },
          },
        };

        setOverrides(updatedOverrides);
      })
      .catch(console.error.bind(console, 'unable to update override'));
  };

  const removeOverride = (flagKey: string, updateState: boolean = true) => {
    return fetch(
      apiRoute(`/dev/projects/${selectedProject}/overrides/${flagKey}`),
      {
        method: 'DELETE',
      },
    )
      .then((res) => {
        // In the remove-all-override case, we need to fan out and make the
        // request for every override, so we don't want to be interleaving
        // local state updates. Expect the consumer to update the local state
        // when all requests are done
        if (res.ok && updateState) {
          const updatedOverrides = { ...overrides };
          delete updatedOverrides[flagKey];

          setOverrides(updatedOverrides);

          if (Object.keys(updatedOverrides).length === 0)
            setOnlyShowOverrides(false);
        }
      })
      .catch(console.error.bind('unable to remove override'));
  };

  const fetchDevFlags = async () => {
    const res = await fetch(
      apiRoute(`/dev/projects/${selectedProject}?expand=overrides`),
    );
    const json = await res.json();
    if (!res.ok) {
      throw new Error(`Got ${res.status}, ${res.statusText} from flag fetch`);
    }

    const { flagsState: flags, overrides } = json;

    setFlags(sortFlags(flags));
    setOverrides(overrides);
  };

  const fetchFlags = async (
    path?: string,
  ): Promise<Record<string, FlagVariation[]>> => {
    if (!path)
      path = `/api/v2/flags/${selectedProject}?summary=false&limit=100`;
    const res = await fetch(`/proxy${path}`);
    if (!res.ok) {
      throw new Error(`Got ${res.status}, ${res.statusText} from flags fetch`);
    }
    const json :FlagsApiResponse= await res.json();
    const flagKeys: string[] = json.items.map((i) => i.key);
    const flagVariations: FlagVariation[][] = json.items.map((i) => i.variations);
    const newAvailableVariations: Record<string,FlagVariation[]> = {};
    for (let i = 0; i < flagKeys.length; i++) {
      newAvailableVariations[flagKeys[i]] = flagVariations[i];
    }
    if (json._links.next)
      return {
        ...(await fetchFlags(json._links.next.href)),
        ...newAvailableVariations,
      };
    else return newAvailableVariations;
  };

  // Fetch flags / overrides on mount
  useEffect(() => {
    Promise.all([
      fetchDevFlags(),
      fetchFlags().then((av) => setAvailableVariations(av)),
    ]).catch(console.error.bind(console, 'error when fetching flags'));
  }, [selectedProject]);

  if (!flags) {
    return null;
  }

  console.log(availableVariations);
  return (
    <>
      <div className="container">
        <Box
          display="flex"
          flexDirection="row"
          justifyContent="space-between"
          alignItems="center"
          marginBottom="2rem"
          padding="1rem"
          background={Theme.color.blue[50]}
          borderRadius={Theme.borderRadius.regular}
        >
          <Label
            htmlFor="only-show-overrides"
            className="only-show-overrides-label"
          >
            <Checkbox
              id="only-show-overrides"
              isSelected={onlyShowOverrides}
              onChange={(newValue) => {
                setOnlyShowOverrides(newValue);
              }}
              isDisabled={!overridesPresent}
              style={{
                display: 'inline-block',
                marginRight: '.25rem',
              }}
            />
            Only show flags with overrides
          </Label>
          <Button
            variant="destructive"
            isDisabled={!overridesPresent}
            onPress={async () => {
              // This button is disabled unless overrides are present, but the
              // type is nullable
              if (!overrides) {
                return;
              }

              const overrideKeys = Object.keys(overrides);

              await Promise.all(
                overrideKeys.map((flagKey) => {
                  // Opt out of local state updates since we're bulk-removing
                  // overrides async
                  removeOverride(flagKey, false);
                }),
              );

              // Winnow out removed overrides and update local state in a
              // single pass
              const updatedOverrides = overrideKeys.reduce(
                (accum, flagKey) => {
                  delete accum[flagKey];

                  return accum;
                },
                { ...overrides },
              );

              setOverrides(updatedOverrides);
              setOnlyShowOverrides(false);
            }}
          >
            <Icon size="medium" name="cancel" />
            Remove all overrides
          </Button>
        </Box>
        <ul className="flags-list">
          {Object.entries(flags).map(
            ([flagKey, { value: flagValue }], index) => {
              const overrideValue = overrides[flagKey]?.value;
              const hasOverride = flagKey in overrides;
              const currentValue = hasOverride ? overrideValue : flagValue;

              if (onlyShowOverrides && !hasOverride) {
                return null;
              }

              return (
                <li
                  key={flagKey}
                  style={{
                    backgroundColor: index % 2 === 0 ? 'white' : '#f8f8f8',
                  }}
                >
                  <Box
                    whiteSpace="nowrap"
                    flexGrow="1"
                    paddingLeft="1rem"
                    paddingRight="1rem"
                  >
                    <CopyToClipboard asChild text={flagKey}>
                      <code className={hasOverride ? 'has-override' : ''}>
                        {flagKey}
                      </code>
                    </CopyToClipboard>
                  </Box>
                  <div className="flag-value">
                    <VariationValues
                      availableVariations={
                        availableVariations[flagKey]
                          ? availableVariations[flagKey]
                          : []
                      }
                      currentValue={currentValue}
                      flagValue={flagValue}
                      flagKey={flagKey}
                      updateOverride={updateOverride}
                    />
                  </div>
                  <Box width="2rem" height="2rem" marginLeft="0.5rem">
                    {hasOverride && (
                      <IconButton
                        icon="cancel"
                        aria-label="Remove override"
                        onPress={() => {
                          removeOverride(flagKey);
                        }}
                        variant="destructive"
                      />
                    )}
                  </Box>
                </li>
              );
            },
          )}
        </ul>
      </div>
    </>
  );
}

export default Flags;
