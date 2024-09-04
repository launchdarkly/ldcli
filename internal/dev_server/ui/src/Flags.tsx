import { LDFlagSet, LDFlagValue } from 'launchdarkly-js-client-sdk';
import {
  Button,
  Checkbox,
  IconButton,
  Label,
  Input,
  SearchField,
  Group,
} from '@launchpad-ui/components';
import {
  Box,
  CopyToClipboard,
  Inline,
  Pagination,
  Stack,
} from '@launchpad-ui/core';
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
  const [searchTerm, setSearchTerm] = useState('');
  const [currentPage, setCurrentPage] = useState(0); // Change initial page to 0
  const flagsPerPage = 20;

  const overridesPresent = useMemo(
    () => overrides && Object.keys(overrides).length > 0,
    [overrides],
  );

  const filteredFlags = useMemo(() => {
    if (!flags) return [];
    const flagEntries = Object.entries(flags);
    const filtered = flagEntries.filter(([flagKey]) => {
      const search = searchTerm.toLowerCase();
      const key = flagKey.toLowerCase();
      let searchIndex = 0;

      // Fuzzy search :P
      for (let i = 0; i < key.length; i++) {
        if (
          key[i] === search[searchIndex] ||
          ((key[i] == '-' || key[i] == '_' || key[i] == '.') &&
            search[searchIndex] == ' ')
        ) {
          searchIndex++;
        }
        if (searchIndex === search.length) {
          return true;
        }
      }
      return false;
    });
    return filtered;
  }, [flags, searchTerm]);

  const paginatedFlags = useMemo(() => {
    const startIndex = currentPage * flagsPerPage; // Adjust startIndex calculation
    const endIndex = startIndex + flagsPerPage;
    return filteredFlags.slice(startIndex, endIndex);
  }, [filteredFlags, currentPage]);

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
      apiRoute(`/dev/projects/${selectedProject}?expand=overrides`),
    );
    const json = await res.json();
    if (!res.ok) {
      throw new Error(`Got ${res.status}, ${res.statusText} from flag fetch`);
    }

    const { flagsState: flags, overrides, sourceEnvironmentKey } = json;

    setFlags(sortFlags(flags));
    setOverrides(overrides);
    setSourceEnvironmentKey(sourceEnvironmentKey);
  }, [selectedProject, setFlags, setSourceEnvironmentKey]);

  const fetchFlags = useCallback(
    async (path?: string): Promise<Record<string, FlagVariation[]>> => {
      if (!path)
        path = `/api/v2/flags/${selectedProject}?summary=false&limit=100`;
      const res = await fetch(`/proxy${path}`);
      if (!res.ok) {
        throw new Error(
          `Got ${res.status}, ${res.statusText} from flags fetch`,
        );
      }
      const json: FlagsApiResponse = await res.json();
      const flagKeys: string[] = json.items.map((i) => i.key);
      const flagVariations: FlagVariation[][] = json.items.map(
        (i) => i.variations,
      );
      const newAvailableVariations: Record<string, FlagVariation[]> = {};
      for (let i = 0; i < flagKeys.length; i++) {
        newAvailableVariations[flagKeys[i]] = flagVariations[i];
      }
      if (json._links.next)
        return {
          ...(await fetchFlags(json._links.next.href)),
          ...newAvailableVariations,
        };
      else return newAvailableVariations;
    },
    [selectedProject],
  );

  // Fetch flags / overrides on mount
  useEffect(() => {
    Promise.all([
      fetchDevFlags(),
      fetchFlags().then((av) => setAvailableVariations(av)),
    ]).catch(console.error.bind(console, 'error when fetching flags'));
  }, [fetchDevFlags, fetchFlags]);

  if (!flags) {
    return null;
  }

  const totalPages = Math.ceil(filteredFlags.length / flagsPerPage);

  const handlePageChange = (direction: string) => {
    switch (direction) {
      case 'next':
        setCurrentPage(
          (prevPage) => Math.min(prevPage + 1, totalPages - 1), // Adjust page increment
        );
        break;
      case 'prev':
        setCurrentPage((prevPage) => Math.max(prevPage - 1, 0)); // Adjust page decrement
        break;
      case 'first':
        setCurrentPage(0); // Adjust first page
        break;
      case 'last':
        setCurrentPage(totalPages - 1); // Adjust last page
        break;
      default:
        break;
    }
  };

  return (
    <>
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
      <Stack gap="4">
        <Inline gap="4">
          <SearchField>
            <Group>
              <Icon name="search" size="small" />
              <Input
                placeholder="Search flags"
                onChange={(e) => {
                  setSearchTerm(e.target.value);
                  setCurrentPage(0); // Reset pagination
                }}
              />
              <IconButton
                aria-label="clear"
                icon="cancel-circle-outline"
                size="small"
                variant="minimal"
              />
            </Group>
          </SearchField>
        </Inline>
        <ul className="flags-list">
          {paginatedFlags.map(([flagKey, { value: flagValue }], index) => {
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
                  height: '2rem',
                  display: 'flex',
                  alignItems: 'center',
                }}
              >
                <Box whiteSpace="nowrap" paddingLeft="1rem" paddingRight="1rem">
                  <Inline gap="2">
                    <CopyToClipboard asChild text={flagKey}>
                      <code className={hasOverride ? 'has-override' : ''}>
                        {flagKey}
                      </code>
                    </CopyToClipboard>

                    {hasOverride && (
                      <Button
                        icon="cancel"
                        aria-label="Remove override"
                        onPress={() => {
                          removeOverride(flagKey);
                        }}
                        variant="destructive"
                      >
                        <Inline gap="2">
                          <Icon name="cancel" size="small" />
                          Remove override
                        </Inline>
                      </Button>
                    )}
                  </Inline>
                </Box>
                <Box
                  alignItems="center"
                  paddingRight="1rem"
                  overflow="hidden"
                  flexShrink={0}
                >
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
                </Box>
              </li>
            );
          })}
        </ul>
      </Stack>
      <div
        style={{
          display: 'flex',
          justifyContent: 'flex-end',
          marginTop: '1rem',
        }}
      >
        <Pagination
          currentOffset={currentPage * flagsPerPage}
          isReady
          onChange={(e) => handlePageChange(e as string)}
          pageSize={flagsPerPage}
          resourceName="flags"
          totalCount={filteredFlags.length}
        />
      </div>
    </>
  );
}

export default Flags;
