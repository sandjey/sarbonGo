--
-- PostgreSQL database dump
--

\restrict KKhasbgcIs7bnzNiYrlc4H4L1PnrGAUpqfSEWiAutPaU7luMLLVkRYySaNl2qGc

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
-- Name: freelance_dispatchers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.freelance_dispatchers (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    name character varying NOT NULL,
    phone character varying NOT NULL,
    password character varying NOT NULL,
    passport_series character varying,
    passport_number character varying,
    pinfl character varying,
    cargo_id uuid,
    driver_id uuid,
    rating double precision,
    work_status character varying,
    account_status character varying,
    photo_path character varying,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone,
    last_online_at timestamp without time zone,
    photo_data bytea,
    photo_content_type character varying(50),
    manager_role character varying(32),
    push_token character varying,
    CONSTRAINT chk_freelance_dispatchers_manager_role CHECK (((manager_role IS NULL) OR ((manager_role)::text = ANY ((ARRAY['CARGO_MANAGER'::character varying, 'DRIVER_MANAGER'::character varying])::text[]))))
);


--
-- Name: freelance_dispatchers freelance_dispatchers_phone_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatchers
    ADD CONSTRAINT freelance_dispatchers_phone_key UNIQUE (phone);


--
-- Name: freelance_dispatchers freelance_dispatchers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatchers
    ADD CONSTRAINT freelance_dispatchers_pkey PRIMARY KEY (id);


--
-- Name: idx_freelance_dispatchers_cargo_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatchers_cargo_id ON public.freelance_dispatchers USING btree (cargo_id);


--
-- Name: idx_freelance_dispatchers_driver_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatchers_driver_id ON public.freelance_dispatchers USING btree (driver_id);


--
-- Name: idx_freelance_dispatchers_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatchers_phone ON public.freelance_dispatchers USING btree (phone);


--
-- Name: idx_freelance_dispatchers_pinfl; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatchers_pinfl ON public.freelance_dispatchers USING btree (pinfl);


--
-- PostgreSQL database dump complete
--

\unrestrict KKhasbgcIs7bnzNiYrlc4H4L1PnrGAUpqfSEWiAutPaU7luMLLVkRYySaNl2qGc

