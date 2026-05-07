import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Divider, Form, Grid, Header } from 'semantic-ui-react';
import {
  API,
  showError,
  showSuccess,
  timestamp2string,
  verifyJSON,
} from '../helpers';

const OperationSetting = () => {
  const { t } = useTranslation();
  let now = new Date();
  let [inputs, setInputs] = useState({
    QuotaForNewUser: 0,
    QuotaForInviter: 0,
    QuotaForInvitee: 0,
    QuotaRemindThreshold: 0,
    PreConsumedQuota: 0,
    ModelRatio: '',
    CompletionRatio: '',
    GroupRatio: '',
    TopUpLink: '',
    ChatLink: '',
    QuotaPerUnit: 0,
    AutomaticDisableChannelEnabled: '',
    AutomaticEnableChannelEnabled: '',
    ChannelDisableThreshold: 0,
    LogConsumeEnabled: '',
    DisplayInCurrencyEnabled: '',
    DisplayTokenStatEnabled: '',
    ApproximateTokenEnabled: '',
    RetryTimes: 0,
    AutoChannelSelectModels: '',
    // 并发限制配置
    EnableManualConcurrencyLimit: '',
    EnableAutoConcurrencyLimit: '',
    UserBaseConcurrentLimit: 5,
    ConcurrencyWaitTimeout: 30,
    ConcurrencyCheckInterval: 100,
    // 自动并发因子权重
    DurationFactorWeight: 25,
    ConcurrentFactorWeight: 25,
    TrendFactorWeight: 20,
    GPUFactorWeight: 30,
    DurationThresholds: '1000:1.0,3000:0.8,5000:0.5,10000:0.1',
    GPUThresholds: '50:1.0,70:0.8,85:0.5,95:0.1,100:0.1',
    // GPU 监控配置
    EnableGPUMonitoring: '',
    VLLMAPIURL: 'http://localhost:8000',
    GPUKVCacheWarn: 85.0,
    GPUKVCacheMax: 95.0,
    // 请求去重配置
    EnableRequestDeduplication: 'true',  // 默认开启
    RequestCacheTTL: 30,
  });

  // 自动并发限制实时状态
  const [autoStatus, setAutoStatus] = useState({
    dynamicLimit: 0,
    manualLimit: 5,
    actualLimit: 5,
    loadLevel: 0,
    factors: { duration: 1.0, concurrent: 1.0, trend: 1.0, gpu: 1.0 },
    metrics: { avgDuration: 0, currentConcurrent: 0, gpuUsage: 0 },
    enabled: { manual: false, auto: false, dedup: false, gpu: false },
  });
  const [originInputs, setOriginInputs] = useState({});
  let [loading, setLoading] = useState(false);
  let [historyTimestamp, setHistoryTimestamp] = useState(
    timestamp2string(now.getTime() / 1000 - 30 * 24 * 3600)
  ); // a month ago

  // Auto channel select state
  let [availableGroups, setAvailableGroups] = useState([]);
  let [selectedGroup, setSelectedGroup] = useState('');
  let [groupModels, setGroupModels] = useState([]);
  let [selectedModels, setSelectedModels] = useState([]);

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      let newInputs = {};
      data.forEach((item) => {
        if (
          item.key === 'ModelRatio' ||
          item.key === 'GroupRatio' ||
          item.key === 'CompletionRatio' ||
          item.key === 'AutoChannelSelectModels'
        ) {
          item.value = JSON.stringify(JSON.parse(item.value), null, 2);
        }
        if (item.value === '{}' || item.value === '[]') {
          item.value = '';
        }
        newInputs[item.key] = item.value;
      });

      // 合并默认配置（确保并发相关配置有默认值）
      const defaultInputs = {
        EnableManualConcurrencyLimit: 'false',
        EnableAutoConcurrencyLimit: 'false',
        UserBaseConcurrentLimit: 5,
        ConcurrencyWaitTimeout: 30,
        ConcurrencyCheckInterval: 100,
        DurationFactorWeight: 25,
        ConcurrentFactorWeight: 25,
        TrendFactorWeight: 20,
        GPUFactorWeight: 30,
        DurationThresholds: '1000:1.0,3000:0.8,5000:0.5,10000:0.1',
        GPUThresholds: '50:1.0,70:0.8,85:0.5,95:0.1,100:0.1',
        EnableGPUMonitoring: 'false',
        VLLMAPIURL: 'http://localhost:8000',
        GPUKVCacheWarn: 85.0,
        GPUKVCacheMax: 95.0,
        EnableRequestDeduplication: 'true',  // 默认开启
        RequestCacheTTL: 30,
      };

      // 合并：使用API返回的值，否则使用默认值
      setInputs({ ...defaultInputs, ...newInputs });
      setOriginInputs({ ...defaultInputs, ...newInputs });
    } else {
      showError(message);
    }
  };

  useEffect(() => {
    getOptions().then();
    loadGroups().then();
    fetchAutoStatus();
  }, []);

  // 获取自动并发限制状态
  const fetchAutoStatus = async () => {
    try {
      const res = await API.get('/api/system/auto-concurrency-limit');
      const { success, data } = res.data;
      if (success && data) {
        setAutoStatus({
          dynamicLimit: data.dynamic_limit || 0,
          manualLimit: data.manual_limit || 5,
          actualLimit: data.actual_limit || 5,
          loadLevel: data.load_level || 0,
          factors: data.factors || { duration: 1.0, concurrent: 1.0, trend: 1.0, gpu: 1.0 },
          weights: data.weights || { duration: 25, concurrent: 25, trend: 20, gpu: 30 },
          metrics: {
            avgDuration: data.metrics?.avg_duration || 0,
            currentConcurrent: data.metrics?.current_concurrent || 0,
            gpuUsage: data.metrics?.gpu_usage || 0,
            runningRequests: data.metrics?.running_requests || 0,
          },
          enabled: data.enabled || { manual: false, auto: false, dedup: true, gpu: false },
        });
      }
    } catch (e) {
      // 忽略错误
    }
  };

  // 自动刷新状态（当自动并发启用时）
  useEffect(() => {
    if (inputs.EnableAutoConcurrencyLimit === 'true') {
      const interval = setInterval(fetchAutoStatus, 5000);
      return () => clearInterval(interval);
    }
  }, [inputs.EnableAutoConcurrencyLimit]);

  // 恢复默认参数
  const resetToDefaults = async () => {
    setLoading(true);
    try {
      const res = await API.post('/api/system/concurrency-reset');
      const { success, message } = res.data;
      if (success) {
        showSuccess(message);
        // 重新加载配置
        await getOptions();
      } else {
        showError(message);
      }
    } catch (err) {
      showError(err.message);
    }
    setLoading(false);
  };

  // 获取负载等级文字
  const getLoadLevelText = (level) => {
    const levels = [t('setting.operation.concurrency.load_level_low'), t('setting.operation.concurrency.load_level_medium'),
      t('setting.operation.concurrency.load_level_high'), t('setting.operation.concurrency.load_level_critical')];
    return levels[level] || levels[0];
  };

  const loadGroups = async () => {
    const res = await API.get('/api/user/groups');
    const { success, message, data } = res.data;
    if (success) {
      const groups = data.map((g) => ({ key: g, text: g, value: g }));
      setAvailableGroups(groups);
      if (groups.length > 0) {
        setSelectedGroup(groups[0].value);
        loadGroupModels(groups[0].value);
      }
    } else {
      showError(message);
    }
  };

  const loadGroupModels = async (group) => {
    const res = await API.get(`/api/group/models?group=${encodeURIComponent(group)}`);
    const { success, message, data } = res.data;
    if (success) {
      setGroupModels(data);
      // Parse current config to set selected models
      try {
        const config = JSON.parse(inputs.AutoChannelSelectModels || '[]');
        const groupConfig = config.find((c) => c.groups && c.groups.includes(group));
        setSelectedModels(groupConfig ? [groupConfig.model] : []);
      } catch {
        setSelectedModels([]);
      }
    } else {
      showError(message);
    }
  };

  const handleGroupChange = (e, { value }) => {
    setSelectedGroup(value);
    loadGroupModels(value);
  };

  const handleModelChange = (e, { value }) => {
    setSelectedModels(value);
  };

  const addModelToConfig = () => {
    if (!selectedGroup || selectedModels.length === 0) {
      showError('请选择分组和模型');
      return;
    }
    let config = [];
    try {
      config = JSON.parse(inputs.AutoChannelSelectModels || '[]');
    } catch {
      config = [];
    }
    // Remove existing config for this group
    config = config.filter((c) => !c.groups || !c.groups.includes(selectedGroup));
    // Add new config
    for (const model of selectedModels) {
      config.push({
        model: model,
        groups: [selectedGroup],
      });
    }
    const newConfig = JSON.stringify(config, null, 2);
    setInputs({ ...inputs, AutoChannelSelectModels: newConfig });
  };

  const removeModelFromConfig = (model, group) => {
    let config = [];
    try {
      config = JSON.parse(inputs.AutoChannelSelectModels || '[]');
    } catch {
      return;
    }
    config = config.filter(
      (c) => !(c.model === model && c.groups && c.groups.includes(group))
    );
    // Always store valid JSON (empty array if no config)
    const newConfig = JSON.stringify(config, null, 2);
    setInputs({ ...inputs, AutoChannelSelectModels: newConfig });
  };

  const updateOption = async (key, value) => {
    setLoading(true);
    const res = await API.put('/api/option/', {
      key,
      value: String(value),
    });
    const { success, message } = res.data;
    if (success) {
      setInputs((inputs) => ({ ...inputs, [key]: String(value) }));
    } else {
      showError(message);
    }
    setLoading(false);
  };

  const handleInputChange = async (e, { name, value, checked }) => {
    if (name.endsWith('Enabled')) {
      // Checkbox 的 value 是静态的，用 checked 代替
      const newValue = checked !== undefined ? String(checked) : value;
      await updateOption(name, newValue);
    } else if (name.includes('FactorWeight') || name.includes('Thresholds')) {
      // 因子权重和阈值配置自动保存
      await updateOption(name, value);
    } else {
      setInputs((inputs) => ({ ...inputs, [name]: value }));
    }
  };

  // Checkbox 专用处理函数（备用，用于内联 onChange）
  const handleCheckboxChange = async (name, checked) => {
    const newValue = String(checked);
    console.log('Checkbox change:', name, '=', newValue);  // 调试日志
    await updateOption(name, newValue);
  };

  const submitConfig = async (group) => {
    switch (group) {
      case 'monitor':
        if (
          originInputs['ChannelDisableThreshold'] !==
          inputs.ChannelDisableThreshold
        ) {
          await updateOption(
            'ChannelDisableThreshold',
            inputs.ChannelDisableThreshold
          );
        }
        if (
          originInputs['QuotaRemindThreshold'] !== inputs.QuotaRemindThreshold
        ) {
          await updateOption(
            'QuotaRemindThreshold',
            inputs.QuotaRemindThreshold
          );
        }
        break;
      case 'concurrency':
        if (originInputs['EnableConcurrencyLimit'] !== inputs.EnableConcurrencyLimit) {
          await updateOption('EnableConcurrencyLimit', inputs.EnableConcurrencyLimit);
        }
        if (originInputs['UserBaseConcurrentLimit'] !== inputs.UserBaseConcurrentLimit) {
          await updateOption('UserBaseConcurrentLimit', inputs.UserBaseConcurrentLimit);
        }
        if (originInputs['ConcurrencyWaitTimeout'] !== inputs.ConcurrencyWaitTimeout) {
          await updateOption('ConcurrencyWaitTimeout', inputs.ConcurrencyWaitTimeout);
        }
        if (originInputs['ConcurrencyCheckInterval'] !== inputs.ConcurrencyCheckInterval) {
          await updateOption('ConcurrencyCheckInterval', inputs.ConcurrencyCheckInterval);
        }
        if (originInputs['EnableGPUMonitoring'] !== inputs.EnableGPUMonitoring) {
          await updateOption('EnableGPUMonitoring', inputs.EnableGPUMonitoring);
        }
        if (originInputs['VLLMAPIURL'] !== inputs.VLLMAPIURL) {
          await updateOption('VLLMAPIURL', inputs.VLLMAPIURL);
        }
        if (originInputs['GPUKVCacheWarn'] !== inputs.GPUKVCacheWarn) {
          await updateOption('GPUKVCacheWarn', inputs.GPUKVCacheWarn);
        }
        if (originInputs['GPUKVCacheMax'] !== inputs.GPUKVCacheMax) {
          await updateOption('GPUKVCacheMax', inputs.GPUKVCacheMax);
        }
        if (originInputs['EnableRequestDeduplication'] !== inputs.EnableRequestDeduplication) {
          await updateOption('EnableRequestDeduplication', inputs.EnableRequestDeduplication);
        }
        if (originInputs['RequestCacheTTL'] !== inputs.RequestCacheTTL) {
          await updateOption('RequestCacheTTL', inputs.RequestCacheTTL);
        }
        break;
      case 'ratio':
        if (originInputs['ModelRatio'] !== inputs.ModelRatio) {
          if (!verifyJSON(inputs.ModelRatio)) {
            showError('模型倍率不是合法的 JSON 字符串');
            return;
          }
          await updateOption('ModelRatio', inputs.ModelRatio);
        }
        if (originInputs['GroupRatio'] !== inputs.GroupRatio) {
          if (!verifyJSON(inputs.GroupRatio)) {
            showError('分组倍率不是合法的 JSON 字符串');
            return;
          }
          await updateOption('GroupRatio', inputs.GroupRatio);
        }
        if (originInputs['CompletionRatio'] !== inputs.CompletionRatio) {
          if (!verifyJSON(inputs.CompletionRatio)) {
            showError('补全倍率不是合法的 JSON 字符串');
            return;
          }
          await updateOption('CompletionRatio', inputs.CompletionRatio);
        }
        break;
      case 'auto_channel_select':
        if (originInputs['AutoChannelSelectModels'] !== inputs.AutoChannelSelectModels) {
          // Accept empty string or valid JSON (empty array if no config)
          const value = inputs.AutoChannelSelectModels || '[]';
          if (value !== '[]' && !verifyJSON(value)) {
            showError('自动渠道选择模型配置不是合法的 JSON 字符串');
            return;
          }
          await updateOption('AutoChannelSelectModels', value);
        }
        break;
      case 'quota':
        if (originInputs['QuotaForNewUser'] !== inputs.QuotaForNewUser) {
          await updateOption('QuotaForNewUser', inputs.QuotaForNewUser);
        }
        if (originInputs['QuotaForInvitee'] !== inputs.QuotaForInvitee) {
          await updateOption('QuotaForInvitee', inputs.QuotaForInvitee);
        }
        if (originInputs['QuotaForInviter'] !== inputs.QuotaForInviter) {
          await updateOption('QuotaForInviter', inputs.QuotaForInviter);
        }
        if (originInputs['PreConsumedQuota'] !== inputs.PreConsumedQuota) {
          await updateOption('PreConsumedQuota', inputs.PreConsumedQuota);
        }
        break;
      case 'general':
        if (originInputs['TopUpLink'] !== inputs.TopUpLink) {
          await updateOption('TopUpLink', inputs.TopUpLink);
        }
        if (originInputs['ChatLink'] !== inputs.ChatLink) {
          await updateOption('ChatLink', inputs.ChatLink);
        }
        if (originInputs['QuotaPerUnit'] !== inputs.QuotaPerUnit) {
          await updateOption('QuotaPerUnit', inputs.QuotaPerUnit);
        }
        if (originInputs['RetryTimes'] !== inputs.RetryTimes) {
          await updateOption('RetryTimes', inputs.RetryTimes);
        }
        break;
    }
  };

  const deleteHistoryLogs = async () => {
    console.log(inputs);
    const res = await API.delete(
      `/api/log/?target_timestamp=${Date.parse(historyTimestamp) / 1000}`
    );
    const { success, message, data } = res.data;
    if (success) {
      showSuccess(`${data} 条日志已清理！`);
      return;
    }
    showError('日志清理失败：' + message);
  };

  return (
    <Grid columns={1}>
      <Grid.Column>
        <Form loading={loading}>
          <Header as='h3'>{t('setting.operation.quota.title')}</Header>
          <Form.Group widths='equal'>
            <Form.Input
              label={t('setting.operation.quota.new_user')}
              name='QuotaForNewUser'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.QuotaForNewUser}
              type='number'
              min='0'
              placeholder={t('setting.operation.quota.new_user_placeholder')}
            />
            <Form.Input
              label={t('setting.operation.quota.pre_consume')}
              name='PreConsumedQuota'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.PreConsumedQuota}
              type='number'
              min='0'
              placeholder={t('setting.operation.quota.pre_consume_placeholder')}
            />
            <Form.Input
              label={t('setting.operation.quota.inviter_reward')}
              name='QuotaForInviter'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.QuotaForInviter}
              type='number'
              min='0'
              placeholder={t(
                'setting.operation.quota.inviter_reward_placeholder'
              )}
            />
            <Form.Input
              label={t('setting.operation.quota.invitee_reward')}
              name='QuotaForInvitee'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.QuotaForInvitee}
              type='number'
              min='0'
              placeholder={t(
                'setting.operation.quota.invitee_reward_placeholder'
              )}
            />
          </Form.Group>
          <Form.Button
            onClick={() => {
              submitConfig('quota').then();
            }}
          >
            {t('setting.operation.quota.buttons.save')}
          </Form.Button>
          <Divider />
          <Header as='h3'>{t('setting.operation.ratio.title')}</Header>
          <Form.Group widths='equal'>
            <Form.TextArea
              label={t('setting.operation.ratio.model.title')}
              name='ModelRatio'
              onChange={handleInputChange}
              style={{ minHeight: 250, fontFamily: 'JetBrains Mono, Consolas' }}
              autoComplete='new-password'
              value={inputs.ModelRatio}
              placeholder={t('setting.operation.ratio.model.placeholder')}
            />
          </Form.Group>
          <Form.Group widths='equal'>
            <Form.TextArea
              label={t('setting.operation.ratio.completion.title')}
              name='CompletionRatio'
              onChange={handleInputChange}
              style={{ minHeight: 250, fontFamily: 'JetBrains Mono, Consolas' }}
              autoComplete='new-password'
              value={inputs.CompletionRatio}
              placeholder={t('setting.operation.ratio.completion.placeholder')}
            />
          </Form.Group>
          <Form.Group widths='equal'>
            <Form.TextArea
              label={t('setting.operation.ratio.group.title')}
              name='GroupRatio'
              onChange={handleInputChange}
              style={{ minHeight: 250, fontFamily: 'JetBrains Mono, Consolas' }}
              autoComplete='new-password'
              value={inputs.GroupRatio}
              placeholder={t('setting.operation.ratio.group.placeholder')}
            />
          </Form.Group>
          <Form.Button
            onClick={() => {
              submitConfig('ratio').then();
            }}
          >
            {t('setting.operation.ratio.buttons.save')}
          </Form.Button>

          <Divider />
          <Header as='h3'>{t('setting.operation.auto_channel_select.title')}</Header>
          <Form.Group widths={3} style={{ alignItems: 'flex-end' }}>
            <Form.Select
              label={t('setting.operation.auto_channel_select.group')}
              options={availableGroups}
              placeholder={t('setting.operation.auto_channel_select.select_group')}
              value={selectedGroup}
              onChange={handleGroupChange}
              selection
              search
            />
            <Form.Select
              label={t('setting.operation.auto_channel_select.model')}
              options={groupModels.map((m) => ({ key: m, text: m, value: m }))}
              placeholder={t('setting.operation.auto_channel_select.select_model')}
              value={selectedModels}
              onChange={handleModelChange}
              selection
              multiple
              search
            />
            <Form.Button
              onClick={addModelToConfig}
              disabled={!selectedGroup || selectedModels.length === 0}
              style={{ height: '38px' }}
            >
              {t('setting.operation.auto_channel_select.add')}
            </Form.Button>
          </Form.Group>

          {/* Current config list */}
          <Form.Group widths='equal'>
            <div style={{ width: '100%' }}>
              <label style={{ display: 'block', marginBottom: '8px', fontWeight: 'bold' }}>
                {t('setting.operation.auto_channel_select.current_config')}
              </label>
              <div style={{ border: '1px solid #e0e0e0', borderRadius: '4px', padding: '10px', minHeight: '60px' }}>
                {(() => {
                  try {
                    const config = JSON.parse(inputs.AutoChannelSelectModels || '[]');
                    if (config.length === 0) {
                      return <span style={{ color: '#999' }}>{t('setting.operation.auto_channel_select.empty')}</span>;
                    }
                    return (
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
                        {config.map((item, idx) => {
                          const groups = item.groups || [];
                          return groups.map((g, gIdx) => (
                            <div key={`${idx}-${gIdx}`} style={{
                              display: 'inline-flex',
                              alignItems: 'center',
                              background: '#e3f2fd',
                              borderRadius: '4px',
                              padding: '4px 8px',
                              gap: '8px'
                            }}>
                              <span>{item.model}</span>
                              <span style={{ color: '#666' }}>({g})</span>
                              <Form.Button
                                onClick={() => removeModelFromConfig(item.model, g)}
                                icon='close'
                                size='mini'
                                color='red'
                                style={{ margin: 0, padding: '2px 6px' }}
                              />
                            </div>
                          ));
                        })}
                      </div>
                    );
                  } catch {
                    return <span style={{ color: '#999' }}>{t('setting.operation.auto_channel_select.empty')}</span>;
                  }
                })()}
              </div>
            </div>
          </Form.Group>
          <Form.Button
            onClick={() => {
              submitConfig('auto_channel_select').then();
            }}
          >
            {t('setting.operation.auto_channel_select.buttons.save')}
          </Form.Button>

          <Divider />
          <Header as='h3'>{t('setting.operation.concurrency.title')}</Header>

          {/* 请求去重设置 */}
          <div style={{ marginBottom: '20px', padding: '15px', backgroundColor: '#f8f9fa', borderRadius: '8px', border: '1px solid #e9ecef' }}>
            <Form.Group inline>
              <Form.Checkbox
                checked={inputs.EnableRequestDeduplication === 'true'}
                label={t('setting.operation.concurrency.dedup_enable')}
                onChange={() => handleCheckboxChange('EnableRequestDeduplication', inputs.EnableRequestDeduplication !== 'true')}
              />
            </Form.Group>
            <p style={{ fontSize: '13px', color: '#666', marginTop: '8px', marginBottom: '12px' }}>
              {t('setting.operation.concurrency.dedup_description')}
            </p>
            <Form.Group widths='equal'>
              <Form.Input
                label={t('setting.operation.concurrency.cache_ttl') + ' (s)'}
                name='RequestCacheTTL'
                onChange={handleInputChange}
                value={inputs.RequestCacheTTL}
                type='number'
                min='5'
                placeholder={t('setting.operation.concurrency.cache_ttl_placeholder')}
              />
            </Form.Group>
          </div>

          {/* 手动并发限制 */}
          <div style={{ marginBottom: '20px', padding: '15px', backgroundColor: '#e8f4f8', borderRadius: '8px', border: '1px solid #b8daff' }}>
            <Form.Group inline>
              <Form.Checkbox
                checked={inputs.EnableManualConcurrencyLimit === 'true'}
                label={t('setting.operation.concurrency.manual_limit_enable')}
                onChange={() => handleCheckboxChange('EnableManualConcurrencyLimit', inputs.EnableManualConcurrencyLimit !== 'true')}
              />
            </Form.Group>
            <Form.Group widths={3} style={{ marginTop: '12px' }}>
              <Form.Input
                label={t('setting.operation.concurrency.max_concurrent')}
                name='UserBaseConcurrentLimit'
                onChange={handleInputChange}
                value={inputs.UserBaseConcurrentLimit}
                type='number'
                min='1'
                max='100'
              />
              <Form.Input
                label={t('setting.operation.concurrency.wait_timeout') + ' (s)'}
                name='ConcurrencyWaitTimeout'
                onChange={handleInputChange}
                value={inputs.ConcurrencyWaitTimeout}
                type='number'
                min='1'
              />
              <Form.Input
                label={t('setting.operation.concurrency.check_interval') + ' (ms)'}
                name='ConcurrencyCheckInterval'
                onChange={handleInputChange}
                value={inputs.ConcurrencyCheckInterval}
                type='number'
                min='10'
              />
            </Form.Group>
          </div>

          {/* 自动并发限制 */}
          <div style={{ marginBottom: '20px', padding: '15px', backgroundColor: '#e8f8e8', borderRadius: '8px', border: '1px solid #c3e6cb' }}>
            <Form.Group inline>
              <Form.Checkbox
                checked={inputs.EnableAutoConcurrencyLimit === 'true'}
                label={t('setting.operation.concurrency.auto_limit_enable')}
                onChange={() => handleCheckboxChange('EnableAutoConcurrencyLimit', inputs.EnableAutoConcurrencyLimit !== 'true')}
              />
            </Form.Group>

            {/* 实时状态显示 */}
            {inputs.EnableAutoConcurrencyLimit === 'true' && (
              <>
                <div style={{ marginTop: '12px', padding: '12px', backgroundColor: '#fff', borderRadius: '6px' }}>
                  <div style={{ display: 'flex', gap: '20px', flexWrap: 'wrap' }}>
                    <div>
                      <span style={{ color: '#666', fontSize: '13px' }}>{t('setting.operation.concurrency.current_dynamic_limit')}: </span>
                      <span style={{ fontWeight: 'bold', fontSize: '18px', color: '#28a745' }}>{autoStatus.dynamicLimit}</span>
                    </div>
                    <div>
                      <span style={{ color: '#666', fontSize: '13px' }}>{t('setting.operation.concurrency.actual_limit')}: </span>
                      <span style={{ fontWeight: 'bold', fontSize: '18px', color: '#dc3545' }}>{autoStatus.actualLimit}</span>
                    </div>
                    <div>
                      <span style={{ color: '#666', fontSize: '13px' }}>{t('setting.operation.concurrency.status_load_level')}: </span>
                      <span style={{ fontWeight: 'bold', color: autoStatus.loadLevel >= 2 ? '#dc3545' : '#28a745' }}>{getLoadLevelText(autoStatus.loadLevel)}</span>
                    </div>
                    <div>
                      <span style={{ color: '#666', fontSize: '13px' }}>{t('setting.operation.concurrency.status_gpu_usage')}: </span>
                      <span style={{ fontWeight: 'bold' }}>{autoStatus.metrics.gpuUsage?.toFixed(1) || 0}%</span>
                    </div>
                  </div>
                  {/* 因子计算值显示 */}
                  <div style={{ marginTop: '10px', padding: '8px', backgroundColor: '#f0f0f0', borderRadius: '4px', fontSize: '12px' }}>
                    <b style={{ display: 'block', marginBottom: '6px', color: '#666' }}>当前因子计算值:</b>
                    <div style={{ display: 'flex', gap: '15px', flexWrap: 'wrap' }}>
                      <span>请求时长: <b>{autoStatus.factors.duration?.toFixed(2)}</b></span>
                      <span>当前并发: <b>{autoStatus.factors.concurrent?.toFixed(2)}</b></span>
                      <span>并发趋势: <b>{autoStatus.factors.trend?.toFixed(2)}</b></span>
                      <span>GPU: <b>{autoStatus.factors.gpu?.toFixed(2)}</b></span>
                    </div>
                  </div>
                </div>

                {/* 因子权重配置 */}
                <div style={{ marginTop: '15px' }}>
                  <b style={{ display: 'block', marginBottom: '10px' }}>{t('setting.operation.concurrency.factor_weights')} <small style={{ fontWeight: 'normal', color: '#666' }}>(权重百分比)</small></b>
                  <Form.Group widths={4}>
                    <Form.Input
                      label={t('setting.operation.concurrency.factor_duration') + ' (%)'}
                      name='DurationFactorWeight'
                      onChange={handleInputChange}
                      value={inputs.DurationFactorWeight}
                      type='number'
                      min='0'
                      max='100'
                    />
                    <Form.Input
                      label={t('setting.operation.concurrency.factor_concurrent') + ' (%)'}
                      name='ConcurrentFactorWeight'
                      onChange={handleInputChange}
                      value={inputs.ConcurrentFactorWeight}
                      type='number'
                      min='0'
                      max='100'
                    />
                    <Form.Input
                      label={t('setting.operation.concurrency.factor_trend') + ' (%)'}
                      name='TrendFactorWeight'
                      onChange={handleInputChange}
                      value={inputs.TrendFactorWeight}
                      type='number'
                      min='0'
                      max='100'
                    />
                    <Form.Input
                      label={t('setting.operation.concurrency.factor_gpu') + ' (%)'}
                      name='GPUFactorWeight'
                      onChange={handleInputChange}
                      value={inputs.GPUFactorWeight}
                      type='number'
                      min='0'
                      max='100'
                    />
                  </Form.Group>
                </div>

              </>
            )}
          </div>

          {/* GPU监控配置 */}
          <div style={{ marginBottom: '20px', padding: '15px', backgroundColor: '#f8f9fa', borderRadius: '8px', border: '1px solid #e9ecef' }}>
            <Form.Group inline>
              <Form.Checkbox
                checked={inputs.EnableGPUMonitoring === 'true'}
                label={t('setting.operation.concurrency.gpu_monitoring_enable')}
                onChange={() => handleCheckboxChange('EnableGPUMonitoring', inputs.EnableGPUMonitoring !== 'true')}
              />
            </Form.Group>
            {inputs.EnableGPUMonitoring === 'true' && (
              <Form.Group widths={3} style={{ marginTop: '12px' }}>
                <Form.Input
                  label={t('setting.operation.concurrency.vllm_api_url')}
                  name='VLLMAPIURL'
                  onChange={handleInputChange}
                  value={inputs.VLLMAPIURL}
                  type='url'
                />
                <Form.Input
                  label={t('setting.operation.concurrency.gpu_kv_warn')}
                  name='GPUKVCacheWarn'
                  onChange={handleInputChange}
                  value={inputs.GPUKVCacheWarn}
                  type='number'
                  min='0'
                  max='100'
                />
                <Form.Input
                  label={t('setting.operation.concurrency.gpu_kv_max')}
                  name='GPUKVCacheMax'
                  onChange={handleInputChange}
                  value={inputs.GPUKVCacheMax}
                  type='number'
                  min='0'
                  max='100'
                />
              </Form.Group>
            )}
          </div>

          <Form.Group>
            <Form.Button
              onClick={() => {
                submitConfig('concurrency').then();
              }}
              primary
              disabled={loading}
            >
              {t('setting.operation.concurrency.buttons.save')}
            </Form.Button>
            <Form.Button
              onClick={resetToDefaults}
              disabled={loading}
            >
              {t('setting.operation.concurrency.reset_defaults')}
            </Form.Button>
          </Form.Group>

          <Divider />
          <Header as='h3'>{t('setting.operation.log.title')}</Header>
          <Form.Group inline>
            <Form.Checkbox
              checked={inputs.LogConsumeEnabled === 'true'}
              label={t('setting.operation.log.enable_consume')}
              name='LogConsumeEnabled'
              onChange={handleInputChange}
            />
          </Form.Group>
          <Form.Group widths={4}>
            <Form.Input
              label={t('setting.operation.log.target_time')}
              value={historyTimestamp}
              type='datetime-local'
              name='history_timestamp'
              onChange={(e, { name, value }) => {
                setHistoryTimestamp(value);
              }}
            />
          </Form.Group>
          <Form.Button
            onClick={() => {
              deleteHistoryLogs().then();
            }}
          >
            {t('setting.operation.log.buttons.clean')}
          </Form.Button>

          <Divider />
          <Header as='h3'>{t('setting.operation.monitor.title')}</Header>
          <Form.Group widths={3}>
            <Form.Input
              label={t('setting.operation.monitor.max_response_time')}
              name='ChannelDisableThreshold'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.ChannelDisableThreshold}
              type='number'
              min='0'
              placeholder={t(
                'setting.operation.monitor.max_response_time_placeholder'
              )}
            />
            <Form.Input
              label={t('setting.operation.monitor.quota_reminder')}
              name='QuotaRemindThreshold'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.QuotaRemindThreshold}
              type='number'
              min='0'
              placeholder={t(
                'setting.operation.monitor.quota_reminder_placeholder'
              )}
            />
          </Form.Group>
          <Form.Group inline>
            <Form.Checkbox
              checked={inputs.AutomaticDisableChannelEnabled === 'true'}
              label={t('setting.operation.monitor.auto_disable')}
              name='AutomaticDisableChannelEnabled'
              onChange={handleInputChange}
            />
            <Form.Checkbox
              checked={inputs.AutomaticEnableChannelEnabled === 'true'}
              label={t('setting.operation.monitor.auto_enable')}
              name='AutomaticEnableChannelEnabled'
              onChange={handleInputChange}
            />
          </Form.Group>
          <Form.Button
            onClick={() => {
              submitConfig('monitor').then();
            }}
          >
            {t('setting.operation.monitor.buttons.save')}
          </Form.Button>

          <Divider />
          <Header as='h3'>{t('setting.operation.general.title')}</Header>
          <Form.Group widths={4}>
            <Form.Input
              label={t('setting.operation.general.topup_link')}
              name='TopUpLink'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.TopUpLink}
              type='link'
              placeholder={t(
                'setting.operation.general.topup_link_placeholder'
              )}
            />
            <Form.Input
              label={t('setting.operation.general.chat_link')}
              name='ChatLink'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.ChatLink}
              type='link'
              placeholder={t('setting.operation.general.chat_link_placeholder')}
            />
            <Form.Input
              label={t('setting.operation.general.quota_per_unit')}
              name='QuotaPerUnit'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.QuotaPerUnit}
              type='number'
              step='0.01'
              placeholder={t(
                'setting.operation.general.quota_per_unit_placeholder'
              )}
            />
            <Form.Input
              label={t('setting.operation.general.retry_times')}
              name='RetryTimes'
              type={'number'}
              step='1'
              min='0'
              onChange={handleInputChange}
              autoComplete='new-password'
              value={inputs.RetryTimes}
              placeholder={t(
                'setting.operation.general.retry_times_placeholder'
              )}
            />
          </Form.Group>
          <Form.Group inline>
            <Form.Checkbox
              checked={inputs.DisplayInCurrencyEnabled === 'true'}
              label={t('setting.operation.general.display_in_currency')}
              name='DisplayInCurrencyEnabled'
              onChange={handleInputChange}
            />
            <Form.Checkbox
              checked={inputs.DisplayTokenStatEnabled === 'true'}
              label={t('setting.operation.general.display_token_stat')}
              name='DisplayTokenStatEnabled'
              onChange={handleInputChange}
            />
            <Form.Checkbox
              checked={inputs.ApproximateTokenEnabled === 'true'}
              label={t('setting.operation.general.approximate_token')}
              name='ApproximateTokenEnabled'
              onChange={handleInputChange}
            />
          </Form.Group>
          <Form.Button
            onClick={() => {
              submitConfig('general').then();
            }}
          >
            {t('setting.operation.general.buttons.save')}
          </Form.Button>
        </Form>
      </Grid.Column>
    </Grid>
  );
};

export default OperationSetting;
