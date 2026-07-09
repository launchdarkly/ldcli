import { useState, useEffect, useMemo } from 'react';
import {
  Label,
  ListBox,
  ListBoxItem,
  Input,
  ProgressBar,
} from '@launchpad-ui/components';
import { Box, Stack } from '@launchpad-ui/core';
import { fetchEnvironments } from './api';
import { Environment } from './types';

// Cached across dialog remounts so reopening doesn't reload; cleared on sync.
const environmentsCache = new Map<string, Environment[]>();

export function clearEnvironmentsCache() {
  environmentsCache.clear();
}

type Props = {
  projectKey: string;
  sourceEnvironmentKey: string | null;
  selectedEnvironment: Environment | null;
  setSelectedEnvironment: (environment: Environment | null) => void;
};

export function EnvironmentSelector({
  projectKey,
  sourceEnvironmentKey,
  selectedEnvironment,
  setSelectedEnvironment,
}: Props) {
  const [environments, setEnvironments] = useState<Environment[] | null>(
    () => environmentsCache.get(projectKey) ?? null,
  );
  const [searchQuery, setSearchQuery] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;

    const cached = environmentsCache.get(projectKey);
    if (cached) {
      setEnvironments(cached);
      return;
    }

    setEnvironments(null);
    setIsLoading(true);
    fetchEnvironments(projectKey)
      .then((envs) => {
        environmentsCache.set(projectKey, envs);
        if (cancelled) {
          return;
        }
        setEnvironments(envs);
        if (!selectedEnvironment) {
          const sourceEnv = envs.find(
            (env) => env.key === sourceEnvironmentKey,
          );
          if (sourceEnv) {
            setSelectedEnvironment(sourceEnv);
          } else if (envs.length > 0) {
            setSelectedEnvironment({
              name: '',
              key: sourceEnvironmentKey || '',
            });
          }
        }
      })
      .catch((error) => {
        console.error('Error fetching environments:', error);
      })
      .finally(() => {
        if (!cancelled) {
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [
    projectKey,
    sourceEnvironmentKey,
    selectedEnvironment,
    setSelectedEnvironment,
  ]);

  const filteredEnvironments = useMemo(() => {
    const query = searchQuery.toLowerCase();
    return (environments ?? []).filter(
      (env) =>
        env.name.toLowerCase().includes(query) ||
        env.key.toLowerCase().includes(query),
    );
  }, [environments, searchQuery]);

  return (
    <Stack gap="3">
      <Box display="flex" justifyContent="space-between" alignItems="center">
        <Label
          htmlFor="environmentSearch"
          style={{ fontSize: '1rem', fontWeight: 'bold' }}
        >
          Environments
        </Label>
      </Box>
      <Box display="flex" justifyContent="space-between" alignItems="center">
        <div style={{ position: 'relative', flexGrow: 1 }}>
          <Input
            id="environmentSearch"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value || '')}
            placeholder="Search environments..."
            aria-label="Search environments"
          />
          {isLoading && (
            <Box
              position="absolute"
              right="0.5rem"
              top="50%"
              transform="translateY(-50%)"
            >
              <ProgressBar size="small" aria-label="Loading environments" />
            </Box>
          )}
        </div>
        <span
          style={{
            whiteSpace: 'nowrap',
            flexShrink: 0,
            marginLeft: '0.5rem',
          }}
        >
          {selectedEnvironment?.name ? (
            selectedEnvironment.name
          ) : (
            <code>{selectedEnvironment?.key}</code>
          )}
        </span>
      </Box>

      <Box
        position="relative"
        height="12.5rem"
        overflow="auto"
        backgroundColor="var(--lp-color-bg-ui-secondary)"
        borderRadius="0.5rem"
        borderStyle="solid"
        borderWidth="0.0625rem"
        borderColor="var(--lp-color-border-ui-primary)"
      >
        <ListBox
          aria-label="Environments"
          selectionMode="single"
          selectedKeys={[selectedEnvironment?.key || '']}
          onSelectionChange={(keys) => {
            const selectedKey = String(Array.from(keys)[0]);
            const selected = environments?.find(
              (env) => env.key === selectedKey,
            );
            if (selected) {
              setSelectedEnvironment(selected);
            }
          }}
        >
          {filteredEnvironments.map((env) => (
            <ListBoxItem key={env.key} id={env.key}>
              {env.name}
            </ListBoxItem>
          ))}
        </ListBox>
      </Box>
    </Stack>
  );
}
