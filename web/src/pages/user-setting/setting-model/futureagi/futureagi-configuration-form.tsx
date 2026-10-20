/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

'use client';

import { zodResolver } from '@hookform/resolvers/zod';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { useFetchFutureAGIConfig } from '@/hooks/use-user-setting-request';
import { IModalProps } from '@/interfaces/common';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';

export const FormId = 'FutureAGIConfigurationForm';

export function FutureAGIConfigurationForm({ onOk }: IModalProps<any>) {
  const { t } = useTranslation();
  const { data } = useFetchFutureAGIConfig();

  const FormSchema = z.object({
    api_key: z
      .string()
      .min(1, { message: t('setting.futureAGIApiKeyMessage') })
      .trim(),
    secret_key: z
      .string()
      .min(1, { message: t('setting.futureAGISecretKeyMessage') })
      .trim(),
    project_name: z
      .string()
      .min(1, { message: t('setting.futureAGIProjectNameMessage') })
      .trim(),
    endpoint: z.string().trim(),
    redact: z.boolean(),
  });

  const form = useForm<z.infer<typeof FormSchema>>({
    resolver: zodResolver(FormSchema),
    defaultValues: {
      api_key: '',
      secret_key: '',
      project_name: '',
      endpoint: '',
      redact: false,
    },
  });

  async function onSubmit(values: z.infer<typeof FormSchema>) {
    onOk?.(values);
  }

  // The server returns the keys masked, so they are never prefilled: enter
  // them again to save.
  useEffect(() => {
    if (data) {
      form.reset({
        api_key: '',
        secret_key: '',
        project_name: data.project_name,
        endpoint: data.endpoint,
        redact: data.redact,
      });
    }
  }, [data, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6"
        id={FormId}
      >
        {data && (
          <p className="text-sm text-text-secondary">
            {t('setting.futureAGISavedKeys', {
              api: data.api_key,
              secret: data.secret_key,
            })}
          </p>
        )}
        <FormField
          control={form.control}
          name="api_key"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('setting.futureAGIApiKey')}</FormLabel>
              <FormControl>
                <Input
                  type={'password'}
                  placeholder={t('setting.futureAGIApiKeyMessage')}
                  {...field}
                  autoComplete="off"
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="secret_key"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('setting.secretKey')}</FormLabel>
              <FormControl>
                <Input
                  type={'password'}
                  placeholder={t('setting.futureAGISecretKeyMessage')}
                  {...field}
                  autoComplete="off"
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="project_name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('setting.futureAGIProjectName')}</FormLabel>
              <FormControl>
                <Input {...field} autoComplete="off" />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="endpoint"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('setting.futureAGIEndpoint')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={'https://api.futureagi.com'}
                  {...field}
                  autoComplete="off"
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="redact"
          render={({ field }) => (
            <FormItem>
              <div className="flex items-center justify-between">
                <FormLabel>{t('setting.futureAGIRedact')}</FormLabel>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </div>
              <FormDescription>
                {t('setting.futureAGIRedactTip')}
              </FormDescription>
            </FormItem>
          )}
        />
      </form>
    </Form>
  );
}
