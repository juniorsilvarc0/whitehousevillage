-- Ordem inversa da `up`: quem referencia cai antes de quem é referenciado.
-- `crm_lost_reasons` é referenciada por `crm_opportunities`, e as duas caem
-- depois das filhas.
--
-- LOSSY, e sem alternativa: o funil, as negociações, as atividades e o
-- histórico de etapa somem. Não há para onde salvá-los — nenhuma tabela do
-- schema anterior sabe representar uma oportunidade.

DROP TABLE IF EXISTS crm_notes;
DROP TABLE IF EXISTS crm_activities;
DROP TABLE IF EXISTS crm_stage_history;
DROP TABLE IF EXISTS crm_opportunity_event_details;
DROP TABLE IF EXISTS crm_opportunities;
DROP TABLE IF EXISTS crm_lost_reasons;
DROP TABLE IF EXISTS crm_leads;
DROP TABLE IF EXISTS crm_stages;
DROP TABLE IF EXISTS crm_pipelines;
