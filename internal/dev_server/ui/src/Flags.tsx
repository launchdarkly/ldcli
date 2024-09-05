import { LDFlagSet, LDFlagValue } from 'launchdarkly-js-client-sdk';
import { Button, Checkbox, IconButton, Label } from '@launchpad-ui/components';
import { Box, CopyToClipboard } from '@launchpad-ui/core';
import Theme from '@launchpad-ui/tokens';
import { useEffect, useState, useCallback, useMemo } from 'react';
import { Icon } from '@launchpad-ui/icons';
import { apiRoute, sortFlags } from './util.ts';
import { FlagsApiResponse, FlagVariation } from './api.ts';
import VariationValues from './Flag.tsx';

type FlagProps = {
  selectedProject: string;
  flags: LDFlagSet | null;
  setFlags: (flags: LDFlagSet) => void;
  setSourceEnvironmentKey: (sourceEnvironmentKey: string) => void;
};

function Flags({
  selectedProject,
  flags,
  setFlags,
  setSourceEnvironmentKey,
}: FlagProps) {
  const [overrides, setOverrides] = useState<
    Record<string, { value: LDFlagValue }>
  >({});
  const [onlyShowOverrides, setOnlyShowOverrides] = useState(false);
  const [availableVariations, setAvailableVariations] = useState<
    Record<string, FlagVariation[]>
  >({});

  const overridesPresent = useMemo(
    () => overrides && Object.keys(overrides).length > 0,
    [overrides],
  );

  const updateOverride = useCallback(
    (flagKey: string, overrideValue: LDFlagValue) => {
      const updatedOverrides = {
        ...overrides,
        ...{
          [flagKey]: {
            value: overrideValue,
          },
        },
      };

      setOverrides(updatedOverrides);
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
        })
        .catch((err) => {
          setOverrides(overrides);
          console.error('unable to update override', err);
        });
    },
    [overrides, selectedProject],
  );

  const removeOverride = useCallback(
    async (flagKey: string) => {
      const updatedOverrides = { ...overrides };
      delete updatedOverrides[flagKey];

      setOverrides(updatedOverrides);

      try {
        const res = await fetch(
          apiRoute(`/dev/projects/${selectedProject}/overrides/${flagKey}`),
          {
            method: 'DELETE',
          },
        );
        if (!res.ok) {
          throw new Error(
            `got ${res.status} ${res.statusText}. ${await res.text()}`,
          );
        }
      } catch (err) {
        console.error('unable to remove override', err);
        setOverrides(overrides);
      }
    },
    [overrides, selectedProject],
  );

  const fetchDevFlags = useCallback(async () => {
    const res = await fetch(
      apiRoute(`/dev/projects/${selectedProject}?expand=overrides&expand=availableVariations`),
    );
    const json = await res.json();
    if (!res.ok) {
      throw new Error(`Got ${res.status}, ${res.statusText} from flag fetch`);
    }

    const { flagsState: flags, overrides, sourceEnvironmentKey, availableVariations } = json;

    setFlags(sortFlags(flags));
    setOverrides(overrides);
    setSourceEnvironmentKey(sourceEnvironmentKey);
    setAvailableVariations(availableVariations);
  }, [selectedProject, setFlags, setSourceEnvironmentKey]);

  // Fetch flags / overrides on mount
  useEffect(() => {
    Promise.all([
      fetchDevFlags(),
    ]).catch(console.error.bind(console, 'error when fetching flags'));
  }, [fetchDevFlags]);

  if (!flags) {
    return null;
  }

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
                  removeOverride(flagKey);
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
