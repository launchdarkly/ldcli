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
import { useState, useCallback, useMemo, useEffect } from 'react';
import { Icon } from '@launchpad-ui/icons';
import { apiRoute } from './util.ts';
import { FlagVariation } from './api.ts';
import VariationValues from './Flag.tsx';
import fuzzysort from 'fuzzysort';

type FlagProps = {
  availableVariations: Record<string, FlagVariation[]>;
  selectedProject: string;
  flags: LDFlagSet | null;
  overrides: Record<string, { value: LDFlagValue; version: number }>;
  setOverrides: (
    overrides: Record<string, { value: LDFlagValue; version: number }>,
  ) => void;
};

function Flags({
  availableVariations,
  selectedProject,
  flags,
  overrides,
  setOverrides,
}: FlagProps) {
  const [onlyShowOverrides, setOnlyShowOverrides] = useState(false);
  const [searchTerm, setSearchTerm] = useState('');
  const [currentPage, setCurrentPage] = useState(0);
  const flagsPerPage = 20;

  const overridesPresent = useMemo(
    () => overrides && Object.keys(overrides).length > 0,
    [overrides],
  );

  useEffect(() => {
    if (!overridesPresent && onlyShowOverrides) {
      setOnlyShowOverrides(false);
    }
  }, [overridesPresent, onlyShowOverrides]);

  const filteredFlags = useMemo(() => {
    if (!flags) return [];
    const flagEntries = Object.entries(flags);
    return flagEntries
      .filter((entry) => {
        if (!searchTerm) return true;
        const [flagKey] = entry;
        const result = fuzzysort.single(searchTerm.toLowerCase(), flagKey);
        return result && result.score > -5000;
      })
      .filter((entry) => {
        const [flagKey] = entry;
        const hasOverride = flagKey in overrides;

        if (onlyShowOverrides && !hasOverride) {
          return false;
        }

        return true;
      });
  }, [flags, searchTerm, onlyShowOverrides, overrides]);

  const paginatedFlags = useMemo(() => {
    const startIndex = currentPage * flagsPerPage;
    const endIndex = startIndex + flagsPerPage;
    return filteredFlags.slice(startIndex, endIndex);
  }, [filteredFlags, currentPage]);

  const updateOverride = useCallback(
    (flagKey: string, overrideValue: LDFlagValue) => {
      const updatedOverrides = {
        ...overrides,
        [flagKey]: {
          value: overrideValue,
          version: overrides[flagKey]?.version || 0,
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

  const removeAllOverrides = useCallback(async () => {
    setOverrides({});
    try {
      const res = await fetch(
        apiRoute(`/dev/projects/${selectedProject}/overrides`),
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
      console.error('unable to remove all overrides', err);
      setOverrides(overrides);
    }
  }, [overrides, selectedProject]);

  if (!flags) {
    return null;
  }

  const totalPages = Math.ceil(filteredFlags.length / flagsPerPage);

  const handlePageChange = (direction: string) => {
    switch (direction) {
      case 'next':
        setCurrentPage((prevPage) => Math.min(prevPage + 1, totalPages - 1));
        break;
      case 'prev':
        setCurrentPage((prevPage) => Math.max(prevPage - 1, 0));
        break;
      case 'first':
        setCurrentPage(0);
        break;
      case 'last':
        setCurrentPage(totalPages - 1);
        break;
      default:
        console.error('invalid page change direction.');
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
        background={'var(--lp-color-bg-feedback-info)'}
        borderStyle="solid"
        borderWidth="0.0625rem"
        borderColor="var(--lp-color-border-feedback-info)"
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
            if (!overrides) {
              return;
            }

            await removeAllOverrides();

            setOverrides({});
            setOnlyShowOverrides(false);
          }}
        >
          <Icon size="medium" name="cancel" />
          Remove all overrides
        </Button>
      </Box>
      <Stack gap="4">
        <Inline gap="4">
          <SearchField aria-label="Search flags">
            <Group>
              <Icon name="search" size="small" />
              <Input
                placeholder="Search flags by key"
                onChange={(e) => {
                  setSearchTerm(e.target.value);
                  setCurrentPage(0);
                }}
                value={searchTerm}
                aria-label="Search flags input"
              />
              <IconButton
                aria-label="clear"
                icon="cancel-circle-outline"
                size="small"
                variant="minimal"
                onPress={() => setSearchTerm('')}
              />
            </Group>
          </SearchField>
        </Inline>
        <ul className="flags-list">
          {paginatedFlags.map(([flagKey, { value: flagValue }], index) => {
            const overrideValue = overrides[flagKey]?.value;
            const hasOverride = flagKey in overrides;
            const currentValue = hasOverride ? overrideValue : flagValue;

            return (
              <li
                key={flagKey}
                style={{
                  backgroundColor:
                    index % 2 === 0
                      ? 'var(--lp-color-bg-ui-primary)'
                      : 'var(--lp-color-bg-ui-secondary)',
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
