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
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    session_id character varying(128) NOT NULL,
    user_id uuid NOT NULL,
    role character varying(32) NOT NULL,
    started_at_utc timestamp with time zone NOT NULL,
    ended_at_utc timestamp with time zone,
    last_seen_at_utc timestamp with time zone,
    duration_seconds bigint DEFAULT 0 NOT NULL,
    device_type character varying(32),
    platform text,
    ip_hash character varying(128),
    geo_city character varying(128),
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL
);


--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (session_id);


--
-- Name: idx_sessions_geo_started; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_geo_started ON public.sessions USING btree (geo_city, started_at_utc DESC);


--
-- Name: idx_sessions_role_started; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_role_started ON public.sessions USING btree (role, started_at_utc DESC);


--
-- Name: idx_sessions_user_started; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_user_started ON public.sessions USING btree (user_id, started_at_utc DESC);


--
-- PostgreSQL database dump complete
--


