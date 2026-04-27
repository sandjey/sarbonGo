--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: analytics_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.analytics_events (
    event_id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    event_name character varying(64) NOT NULL,
    event_time_utc timestamp with time zone DEFAULT now() NOT NULL,
    session_id character varying(128),
    user_id uuid,
    role character varying(32) NOT NULL,
    entity_type character varying(64),
    entity_id uuid,
    actor_id uuid,
    device_type character varying(32),
    platform text,
    ip_hash character varying(128),
    geo_city character varying(128),
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL
);


--
-- Name: analytics_events analytics_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.analytics_events
    ADD CONSTRAINT analytics_events_pkey PRIMARY KEY (event_id);


--
-- Name: idx_analytics_events_entity_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_entity_time ON public.analytics_events USING btree (entity_type, entity_id, event_time_utc DESC);


--
-- Name: idx_analytics_events_geo_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_geo_time ON public.analytics_events USING btree (geo_city, event_time_utc DESC);


--
-- Name: idx_analytics_events_name_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_name_time ON public.analytics_events USING btree (event_name, event_time_utc DESC);


--
-- Name: idx_analytics_events_role_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_role_time ON public.analytics_events USING btree (role, event_time_utc DESC);


--
-- Name: idx_analytics_events_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_time ON public.analytics_events USING btree (event_time_utc DESC);


--
-- Name: idx_analytics_events_user_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_analytics_events_user_time ON public.analytics_events USING btree (user_id, event_time_utc DESC);


--
-- PostgreSQL database dump complete
--


